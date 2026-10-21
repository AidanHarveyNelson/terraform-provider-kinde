// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccOrganizationFeatureFlagsResource(t *testing.T) {
	prefix := strings.ReplaceAll(acctest.RandomWithPrefix("tfacc_org"), "-", "_")
	boolKey, strKey := prefix+"_bool", prefix+"_str"

	config := func(flags string) string {
		return fmt.Sprintf(`
resource "kinde_organization" "test" {
  name = %[1]q
}

resource "kinde_feature_flag" "bool" {
  key                  = %[2]q
  name                 = %[2]q
  type                 = "bool"
  allow_override_level = "org"
  default_value        = "false"
}

resource "kinde_feature_flag" "str" {
  key                  = %[3]q
  name                 = %[3]q
  type                 = "str"
  allow_override_level = "org"
  default_value        = "default"
}

resource "kinde_organization_feature_flags" "test" {
  org_code = kinde_organization.test.code
  flags    = %[4]s
}
`, prefix, boolKey, strKey, flags)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckFeatureFlagGone(boolKey),
			testAccCheckFeatureFlagGone(strKey),
		),
		Steps: []resource.TestStep{
			{
				Config: config(`{
    (kinde_feature_flag.bool.key) = "true"
    (kinde_feature_flag.str.key)  = "custom"
  }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_organization_feature_flags.test", "id", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags.%", "2"),
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags."+boolKey, "true"),
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags."+strKey, "custom"),
				),
			},
			{
				Config: config(`{
    (kinde_feature_flag.bool.key) = "false"
  }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags.%", "1"),
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags."+boolKey, "false"),
				),
			},
			{
				ResourceName:      "kinde_organization_feature_flags.test",
				ImportState:       true,
				ImportStateIdFunc: testAccOrganizationFeatureFlagsImportID(boolKey),
				ImportStateVerify: true,
			},
		},
	})
}

func testAccOrganizationFeatureFlagsImportID(keys string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources["kinde_organization_feature_flags.test"]
		if !ok {
			return "", fmt.Errorf("kinde_organization_feature_flags.test not found in state")
		}
		return rs.Primary.Attributes["org_code"] + ":" + keys, nil
	}
}

func testAccOrganizationFeatureFlagsConfig(orgCode string, flags map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "resource \"kinde_organization_feature_flags\" \"test\" {\n  org_code = %q\n  flags = {\n", orgCode)
	for _, key := range sortedKeys(flags) {
		fmt.Fprintf(&b, "    %q = %q\n", key, flags[key])
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

func TestOrganizationFeatureFlagsResourceAgainstStubServer(t *testing.T) {
	newOrgFlags := func() *stubFeatureFlags {
		return &stubFeatureFlags{
			types:     map[string]string{"beta": "bool", "seats": "int"},
			defaults:  map[string]string{"beta": "false", "seats": "5", "theme": "default", "unmanaged": "default"},
			overrides: map[string]string{"unmanaged": "set outside terraform"},
		}
	}
	acme, globex := newOrgFlags(), newOrgFlags()
	handlers := acme.handlersAt("/api/v1/organizations/org_acme/feature_flags", true, true)
	maps.Copy(handlers, globex.handlersAt("/api/v1/organizations/org_globex/feature_flags", true, true))
	newStubManagementServer(t, handlers)

	expectOverride := func(org *stubFeatureFlags, key, want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got, ok := org.override(key)
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
			expectOverride(globex, "beta", ""),
			expectOverride(globex, "unmanaged", "set outside terraform"),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationFeatureFlagsConfig("org_acme", map[string]string{"beta": "true", "seats": "10"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "id", "org_acme"),
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "flags.beta", "true"),
					expectOverride(acme, "beta", "true"),
					expectOverride(acme, "seats", "10"),
				),
			},
			{
				Config: testAccOrganizationFeatureFlagsConfig("org_acme", map[string]string{"beta": "false", "theme": "pink"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("kinde_organization_feature_flags.test", "flags.seats"),
					expectOverride(acme, "beta", "false"),
					expectOverride(acme, "seats", ""),
					expectOverride(acme, "theme", "pink"),
					expectOverride(acme, "unmanaged", "set outside terraform"),
				),
			},
			{
				PreConfig:          func() { acme.setOverride("theme", "changed in kinde") },
				Config:             testAccOrganizationFeatureFlagsConfig("org_acme", map[string]string{"beta": "false", "theme": "pink"}),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccOrganizationFeatureFlagsConfig("org_acme", map[string]string{"beta": "false", "theme": "pink"}),
				Check:  expectOverride(acme, "theme", "pink"),
			},
			{
				ResourceName:      "kinde_organization_feature_flags.test",
				ImportState:       true,
				ImportStateId:     "org_acme:beta, theme",
				ImportStateVerify: true,
			},
			{
				Config: testAccOrganizationFeatureFlagsConfig("org_globex", map[string]string{"beta": "true"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_feature_flags.test", "id", "org_globex"),
					expectOverride(acme, "beta", ""),
					expectOverride(acme, "theme", ""),
					expectOverride(globex, "beta", "true"),
				),
			},
		},
	})
}

func TestOrganizationFeatureFlagsResourceRejectsBadImportID(t *testing.T) {
	newStubManagementServer(t, map[string]http.HandlerFunc{})

	for _, id := range []string{"org_acme", ":beta", "org_acme:"} {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:        testAccOrganizationFeatureFlagsConfig("org_acme", map[string]string{"beta": "true"}),
					ResourceName:  "kinde_organization_feature_flags.test",
					ImportState:   true,
					ImportStateId: id,
					ExpectError:   regexp.MustCompile(`Invalid Import ID`),
				},
			},
		})
	}
}
