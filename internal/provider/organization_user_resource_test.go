// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccOrganizationUserResource_Roles(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	key := strings.ReplaceAll(testID, "-", "_")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationUserRolesConfig(testID, key, "first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.first", "id"),
				),
			},
			{
				Config: testAccOrganizationUserRolesConfig(testID, key, "second"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.second", "id"),
				),
			},
			{
				ResourceName:      "kinde_organization_user.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccOrganizationUserRolesConfig(testID, key, role string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
  name = "%[1]s-org"
}

resource "kinde_user" "test" {
  first_name = "Org"
  last_name  = "Member"
  identities = [
    {
      type  = "email"
      value = "org.member.%[1]s@example.com"
    }
  ]
}

resource "kinde_role" "first" {
  name        = "%[1]s-first"
  key         = "%[2]s_first"
  description = "First membership role"
}

resource "kinde_role" "second" {
  name        = "%[1]s-second"
  key         = "%[2]s_second"
  description = "Second membership role"
}

resource "kinde_organization_user" "test" {
  organization_code = kinde_organization.test.code
  user_id           = kinde_user.test.id
  roles             = [kinde_role.%[3]s.id]
}
`, testID, key, role)
}
