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
		CheckDestroy:             testAccCheckKindeUserRolesDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccUserRoleConfig(testID, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "role_id", "kinde_role.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrSet("kinde_user_role.test", "id"),
					testAccCheckKindeUserRole("kinde_user_role.test"),
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

func testAccUserRoleConfig(testID, key string) string {
	return fmt.Sprintf(`
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
`, testID, key)
}

func TestUserRoleResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	config := testAccUserRoleConfig("offline", "offline")
	var assignmentID string

	expectAssigned := func(state *terraformState) error {
		orgCode, err := stateAttr(state, "kinde_user_role.test", "organization_code")
		if err != nil {
			return err
		}
		userID, _ := stateAttr(state, "kinde_user_role.test", "user_id")
		roleID, _ := stateAttr(state, "kinde_user_role.test", "role_id")
		return fake.check(func() error {
			for _, assigned := range fake.orgMembers[orgCode][userID] {
				if assigned == roleID {
					return nil
				}
			}
			return fmt.Errorf("role %s is not assigned to %s in Kinde", roleID, userID)
		})(state)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if !fake.removedRoles[assignmentID] {
				return fmt.Errorf("role assignment %s was not removed in Kinde on destroy", assignmentID)
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					expectAssigned,
					captureAttr("kinde_user_role.test", "id", &assignmentID),
				),
			},
			{
				ResourceName:      "kinde_user_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			offlineNotFoundStep(config, func() {
				fake.do(func() {
					for orgCode, members := range fake.orgMembers {
						for userID := range members {
							fake.orgMembers[orgCode][userID] = nil
						}
					}
				})
			}),
			{
				Config: config,
				Check:  expectAssigned,
			},
		},
	})
}
