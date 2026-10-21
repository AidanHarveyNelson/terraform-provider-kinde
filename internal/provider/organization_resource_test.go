// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccOrganizationResource(t *testing.T) {
	testName := acctest.RandomWithPrefix("tfacc")
	var code string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeOrganizationsDestroyed,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccOrganizationResourceConfig(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "id"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "created_on"),
					testAccCheckKindeOrganization("kinde_organization.test", testName),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName+"-updated"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					testAccCheckKindeOrganization("kinde_organization.test", testName+"-updated"),
					captureAttr("kinde_organization.test", "code", &code),
				),
			},
			{
				PreConfig: testAccDeleteOutsideTerraform(t, "organization", func(ctx context.Context, client *ManagementAPI) error {
					return (&OrganizationResource{client: client}).deleteOrganization(ctx, code)
				}),
				Config:             testAccOrganizationResourceConfigUpdate(testName),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check:  testAccCheckKindeOrganization("kinde_organization.test", testName+"-updated"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccOrganizationResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}
`, name)
}

func testAccOrganizationResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = "%[1]s-updated"
}
`, name)
}

func testOrganizationOfflineConfig(name, color, theme string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
  name              = %[1]q
  handle            = "acme"
  external_id       = "external-1"
  background_color  = %[2]q
  button_color      = %[2]q
  button_text_color = "#ffffff"
  link_color        = %[2]q
  theme_code        = %[3]q
}
`, name, color, theme)
}

func TestOrganizationResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	first := testOrganizationOfflineConfig("Acme", "#111111", "light")
	updated := testOrganizationOfflineConfig("Acme Updated", "#222222", "dark")

	expectOrganization := func(name, color, theme string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			code, err := stateAttr(state, "kinde_organization.test", "code")
			if err != nil {
				return err
			}
			return fake.check(func() error {
				organization, ok := fake.organizations[code]
				if !ok {
					return fmt.Errorf("organization %s not found in Kinde", code)
				}
				if organization.Name != name || organization.ColorScheme != theme ||
					organization.Handle == nil || *organization.Handle != "acme" ||
					organization.ExternalID == nil || *organization.ExternalID != "external-1" ||
					organization.BackgroundColor == nil || organization.BackgroundColor.Hex != color ||
					organization.ButtonColor == nil || organization.ButtonColor.Hex != color ||
					organization.LinkColor == nil || organization.LinkColor.Hex != color ||
					organization.ButtonTextColor == nil || organization.ButtonTextColor.Hex != "#ffffff" {
					return fmt.Errorf("unexpected organization in Kinde: %+v", *organization)
				}
				return nil
			})(state)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if len(fake.organizations) != 0 {
				return fmt.Errorf("%d organizations still exist", len(fake.organizations))
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_organization.test", "id", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "created_on"),
					resource.TestCheckResourceAttr("kinde_organization.test", "background_color", "#111111"),
					expectOrganization("Acme", "#111111", "light"),
				),
			},
			{
				ResourceName:      "kinde_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", "Acme Updated"),
					resource.TestCheckResourceAttr("kinde_organization.test", "theme_code", "dark"),
					expectOrganization("Acme Updated", "#222222", "dark"),
				),
			},
			offlineNotFoundStep(updated, func() {
				fake.do(func() {
					for code := range fake.organizations {
						delete(fake.organizations, code)
					}
				})
			}),
			{
				Config: updated,
				Check:  expectOrganization("Acme Updated", "#222222", "dark"),
			},
		},
	})
}
