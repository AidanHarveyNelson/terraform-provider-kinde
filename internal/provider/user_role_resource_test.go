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

func TestAccUserRoleResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	key := strings.ReplaceAll(testID, "-", "_")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "kinde_organization" "test" {
  name = "%[1]s-org"
}

resource "kinde_user" "test" {
  first_name = "Role"
  last_name  = "Holder"
  identities = [
    {
      type  = "email"
      value = "role.holder.%[1]s@example.com"
    }
  ]
}

resource "kinde_role" "test" {
  name        = "%[1]s-role"
  key         = "%[2]s_role"
  description = "Role assigned by the user_role test"
}

resource "kinde_organization_user" "test" {
  organization_code = kinde_organization.test.code
  user_id           = kinde_user.test.id

  lifecycle {
    ignore_changes = [roles]
  }
}

resource "kinde_user_role" "test" {
  organization_code = kinde_organization_user.test.organization_code
  user_id           = kinde_organization_user.test.user_id
  role_id           = kinde_role.test.id
}
`, testID, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "role_id", "kinde_role.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrSet("kinde_user_role.test", "id"),
				),
			},
			{
				ResourceName:      "kinde_user_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
