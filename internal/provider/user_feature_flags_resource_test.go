// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserFeatureFlagsResource(t *testing.T) {
	prefix := strings.ReplaceAll(acctest.RandomWithPrefix("tfacc_usr"), "-", "_")
	boolKey, intKey := prefix+"_bool", prefix+"_int"

	config := func(flags string) string {
		return fmt.Sprintf(`
resource "kinde_user" "test" {
  first_name = "Feature"
  last_name  = "Flags"

  identities = [
    {
      type  = "email"
      value = "%[1]s@example.com"
    }
  ]
}

resource "kinde_feature_flag" "bool" {
  key                  = %[2]q
  name                 = %[2]q
  type                 = "bool"
  allow_override_level = "usr"
  default_value        = "false"
}

resource "kinde_feature_flag" "int" {
  key                  = %[3]q
  name                 = %[3]q
  type                 = "int"
  allow_override_level = "usr"
  default_value        = "1"
}

resource "kinde_user_feature_flags" "test" {
  user_id = kinde_user.test.id
  flags   = %[4]s
}
`, strings.ReplaceAll(prefix, "_", "-"), boolKey, intKey, flags)
	}

	scope := testAccUserScope()
	userID := ""
	twoFlags := config(`{
    (kinde_feature_flag.bool.key) = "true"
    (kinde_feature_flag.int.key)  = "42"
  }`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckFeatureFlagGone(boolKey),
			testAccCheckFeatureFlagGone(intKey),
		),
		Steps: []resource.TestStep{
			{
				Config: twoFlags,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_user_feature_flags.test", "id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags.%", "2"),
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags."+boolKey, "true"),
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags."+intKey, "42"),
					testAccCaptureAttr("kinde_user.test", "id", &userID),
					testAccCheckKindeFlagValues(scope, &userID, map[string]string{boolKey: "true", intKey: "42"}),
				),
			},
			{
				PreConfig:          func() { testAccSetKindeOverride(t, scope, &userID, intKey, "7") },
				Config:             twoFlags,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: twoFlags,
				Check:  testAccCheckKindeFlagValues(scope, &userID, map[string]string{intKey: "42"}),
			},
			{
				Config: config(`{
    (kinde_feature_flag.bool.key) = "false"
  }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags.%", "1"),
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags."+boolKey, "false"),
					resource.TestCheckNoResourceAttr("kinde_user_feature_flags.test", "flags."+intKey),
					testAccCheckKindeFlagValues(scope, &userID, map[string]string{boolKey: "false", intKey: "42"}),
				),
			},
			{
				ResourceName:      "kinde_user_feature_flags.test",
				ImportState:       true,
				ImportStateIdFunc: testAccUserFeatureFlagsImportID(boolKey),
				ImportStateVerify: true,
			},
		},
	})
}

func testAccUserFeatureFlagsImportID(keys string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources["kinde_user_feature_flags.test"]
		if !ok {
			return "", fmt.Errorf("kinde_user_feature_flags.test not found in state")
		}
		return rs.Primary.Attributes["user_id"] + ":" + keys, nil
	}
}

func testAccUserFeatureFlagsConfig(userID string, flags map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "resource \"kinde_user_feature_flags\" \"test\" {\n  user_id = %q\n  flags = {\n", userID)
	for _, key := range sortedKeys(flags) {
		fmt.Fprintf(&b, "    %q = %q\n", key, flags[key])
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

func TestUserFeatureFlagsResourceAgainstStubServer(t *testing.T) {
	flags := &stubFeatureFlags{
		types:     map[string]string{"beta": "bool", "seats": "int"},
		defaults:  map[string]string{"beta": "false", "seats": "5", "theme": "default"},
		overrides: map[string]string{},
	}
	handlers := flags.handlersAt("/api/v1/users/kp_123/feature_flags", true, false)
	var deletes atomic.Int32
	for path, handler := range handlers {
		if path == "/api/v1/users/kp_123/feature_flags" {
			continue
		}
		handlers[path] = func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				deletes.Add(1)
			}
			handler(w, r)
		}
	}
	newStubManagementServer(t, handlers)

	expectOverride := func(key, want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if got, _ := flags.override(key); got != want {
				return fmt.Errorf("expected override %s=%q, got %q", key, want, got)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			expectOverride("beta", "false"),
			expectOverride("theme", "pink"),
			func(*terraform.State) error {
				if n := deletes.Load(); n != 0 {
					return fmt.Errorf("expected no DELETE requests, got %d", n)
				}
				return nil
			},
		),
		Steps: []resource.TestStep{
			{
				Config: testAccUserFeatureFlagsConfig("kp_123", map[string]string{"beta": "true", "seats": "10"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "id", "kp_123"),
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "user_id", "kp_123"),
					expectOverride("beta", "true"),
					expectOverride("seats", "10"),
				),
			},
			{
				Config: testAccUserFeatureFlagsConfig("kp_123", map[string]string{"beta": "false", "theme": "pink"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user_feature_flags.test", "flags.%", "2"),
					resource.TestCheckNoResourceAttr("kinde_user_feature_flags.test", "flags.seats"),
					expectOverride("beta", "false"),
					expectOverride("seats", "10"),
					expectOverride("theme", "pink"),
				),
			},
			{
				PreConfig:          func() { flags.setOverride("theme", "changed in kinde") },
				Config:             testAccUserFeatureFlagsConfig("kp_123", map[string]string{"beta": "false", "theme": "pink"}),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccUserFeatureFlagsConfig("kp_123", map[string]string{"beta": "false", "theme": "pink"}),
				Check:  expectOverride("theme", "pink"),
			},
			{
				ResourceName:      "kinde_user_feature_flags.test",
				ImportState:       true,
				ImportStateId:     "kp_123:beta,theme",
				ImportStateVerify: true,
			},
		},
	})
}

func TestUserFeatureFlagsResourceReportsFailedCreate(t *testing.T) {
	flags := &stubFeatureFlags{
		defaults:  map[string]string{"alpha": "false"},
		overrides: map[string]string{},
	}
	handlers := flags.handlersAt("/api/v1/users/kp_123/feature_flags", true, false)
	handlers["/api/v1/users/kp_123/feature_flags/missing"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]string{{"code": "FEATURE_FLAG_NOT_FOUND", "message": "Feature flag not found"}},
		})
	}
	newStubManagementServer(t, handlers)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccUserFeatureFlagsConfig("kp_123", map[string]string{"alpha": "true", "missing": "x"}),
				ExpectError: regexp.MustCompile(`FEATURE_FLAG_NOT_FOUND`),
			},
		},
	})

	if value, _ := flags.override("alpha"); value != "true" {
		t.Fatalf("expected the alpha override to remain, since user overrides can't be removed, got %q", value)
	}
}
