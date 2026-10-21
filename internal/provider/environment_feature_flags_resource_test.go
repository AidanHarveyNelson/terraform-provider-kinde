// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/kinde-go"
)

func TestAccEnvironmentFeatureFlagsResource(t *testing.T) {
	boolKey := strings.ReplaceAll(acctest.RandomWithPrefix("tfacc_bool"), "-", "_")
	strKey := strings.ReplaceAll(acctest.RandomWithPrefix("tfacc_str"), "-", "_")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccCreateFeatureFlag(t, boolKey, management_api.CreateFeatureFlagReqTypeBool, "false")
			testAccCreateFeatureFlag(t, strKey, management_api.CreateFeatureFlagReqTypeStr, "default")
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccEnvironmentFeatureFlagsConfig(map[string]string{boolKey: "true", strKey: "first"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "id", "environment"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags.%", "2"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags."+boolKey, "true"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags."+strKey, "first"),
				),
			},
			{
				Config: testAccEnvironmentFeatureFlagsConfig(map[string]string{boolKey: "false"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags.%", "1"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags."+boolKey, "false"),
				),
			},
			{
				ResourceName:      "kinde_environment_feature_flags.test",
				ImportState:       true,
				ImportStateId:     boolKey,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCreateFeatureFlag(t *testing.T, key string, flagType management_api.CreateFeatureFlagReqType, defaultValue string) {
	t.Helper()
	ctx := context.Background()

	client, err := newManagementClient(ctx, kinde.NewClientOptions())
	if err != nil {
		t.Fatalf("failed to create management client: %s", err)
	}

	res, err := client.CreateFeatureFlag(ctx, &management_api.CreateFeatureFlagReq{
		Name:               key,
		Key:                key,
		Type:               flagType,
		AllowOverrideLevel: management_api.NewOptCreateFeatureFlagReqAllowOverrideLevel(management_api.CreateFeatureFlagReqAllowOverrideLevelEnv),
		DefaultValue:       defaultValue,
	})
	if err != nil {
		t.Fatalf("failed to create feature flag %s: %s", key, err)
	}
	if _, ok := res.(*management_api.SuccessResponse); !ok {
		t.Fatalf("failed to create feature flag %s: %s", key, managementAPIError("create feature flag", res))
	}

	t.Cleanup(func() {
		if err := deleteFeatureFlag(ctx, client, key); err != nil {
			t.Logf("failed to delete feature flag %s: %s", key, err)
		}
	})
}

func testAccEnvironmentFeatureFlagsConfig(flags map[string]string) string {
	var b strings.Builder
	b.WriteString("resource \"kinde_environment_feature_flags\" \"test\" {\n  flags = {\n")
	for _, key := range sortedKeys(flags) {
		fmt.Fprintf(&b, "    %q = %q\n", key, flags[key])
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

type stubFeatureFlags struct {
	mu        sync.Mutex
	types     map[string]string
	defaults  map[string]string
	overrides map[string]string
}

func (s *stubFeatureFlags) typedValue(key, value string) any {
	switch s.types[key] {
	case "bool":
		return value == "true"
	case "int":
		return json.Number(value)
	case "json":
		return json.RawMessage(value)
	default:
		return value
	}
}

func (s *stubFeatureFlags) override(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.overrides[key]
	return value, ok
}

func (s *stubFeatureFlags) setOverride(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[key] = value
}

func (s *stubFeatureFlags) handlers() map[string]http.HandlerFunc {
	handlers := map[string]http.HandlerFunc{
		"/api/v1/environment/feature_flags": func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			flags := map[string]any{}
			for key, value := range s.defaults {
				if override, ok := s.overrides[key]; ok {
					value = override
				}
				flagType := s.types[key]
				if flagType == "" {
					flagType = "str"
				}
				flags[key] = map[string]any{"type": flagType, "value": s.typedValue(key, value)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "OK", "feature_flags": flags})
		},
	}

	for key := range s.defaults {
		handlers["/api/v1/environment/feature_flags/"+key] = func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			switch r.Method {
			case http.MethodPatch:
				var body struct {
					Value string `json:"value"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				s.overrides[key] = body.Value
			case http.MethodDelete:
				delete(s.overrides, key)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "OK", "message": "Success"})
		}
	}

	return handlers
}

func TestEnvironmentFeatureFlagsResourceAgainstStubServer(t *testing.T) {
	flags := &stubFeatureFlags{
		types:     map[string]string{"alpha": "bool", "beta": "int", "options-json": "json"},
		defaults:  map[string]string{"alpha": "false", "beta": "1", "gamma": "default", "options-json": `{"a":1}`, "unmanaged": "default"},
		overrides: map[string]string{"unmanaged": "set outside terraform"},
	}
	newStubManagementServer(t, flags.handlers())

	expectOverride := func(key, want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got, ok := flags.override(key)
			if want == "" && ok {
				return fmt.Errorf("expected no override for %s, got %q", key, got)
			}
			if want != "" && got != want {
				return fmt.Errorf("expected override %s=%q, got %q (set: %t)", key, want, got, ok)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			expectOverride("alpha", ""),
			expectOverride("gamma", ""),
			expectOverride("unmanaged", "set outside terraform"),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccEnvironmentFeatureFlagsConfig(map[string]string{"alpha": "true", "beta": "2", "options-json": `{"b":2,"a":1}`}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "id", "environment"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags.alpha", "true"),
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags.beta", "2"),
					expectOverride("alpha", "true"),
					expectOverride("beta", "2"),
				),
			},
			{
				Config: testAccEnvironmentFeatureFlagsConfig(map[string]string{"alpha": "false", "gamma": "custom"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_environment_feature_flags.test", "flags.%", "2"),
					resource.TestCheckNoResourceAttr("kinde_environment_feature_flags.test", "flags.beta"),
					expectOverride("alpha", "false"),
					expectOverride("beta", ""),
					expectOverride("gamma", "custom"),
					expectOverride("unmanaged", "set outside terraform"),
				),
			},
			{
				PreConfig:          func() { flags.setOverride("gamma", "changed in kinde") },
				Config:             testAccEnvironmentFeatureFlagsConfig(map[string]string{"alpha": "false", "gamma": "custom"}),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccEnvironmentFeatureFlagsConfig(map[string]string{"alpha": "false", "gamma": "custom"}),
				Check:  expectOverride("gamma", "custom"),
			},
			{
				ResourceName:      "kinde_environment_feature_flags.test",
				ImportState:       true,
				ImportStateId:     "alpha, gamma",
				ImportStateVerify: true,
			},
		},
	})
}

func TestEnvironmentFeatureFlagsResourceRollsBackFailedCreate(t *testing.T) {
	flags := &stubFeatureFlags{
		defaults:  map[string]string{"alpha": "false"},
		overrides: map[string]string{},
	}
	handlers := flags.handlers()
	handlers["/api/v1/environment/feature_flags/missing"] = func(w http.ResponseWriter, r *http.Request) {
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
				Config:      testAccEnvironmentFeatureFlagsConfig(map[string]string{"alpha": "true", "missing": "x"}),
				ExpectError: regexp.MustCompile(`FEATURE_FLAG_NOT_FOUND`),
			},
		},
	})

	if value, ok := flags.override("alpha"); ok {
		t.Fatalf("expected the alpha override to be rolled back, got %q", value)
	}
}

func TestFeatureFlagValueString(t *testing.T) {
	cases := map[string]string{
		`"pink"`:                        "pink",
		`true`:                          "true",
		`false`:                         "false",
		`42`:                            "42",
		`12345678901234567890`:          "12345678901234567890",
		`{"b":[],"a":false}`:            `{"a":false,"b":[]}`,
		`null`:                          "",
		`{"url":"https://x.io/?a=1&b"}`: `{"url":"https://x.io/?a=1\u0026b"}`,
	}

	for raw, want := range cases {
		got, err := featureFlagValueString(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("featureFlagValueString(%s) returned error: %s", raw, err)
		}
		if got != want {
			t.Errorf("featureFlagValueString(%s) = %q, want %q", raw, got, want)
		}
	}
}
