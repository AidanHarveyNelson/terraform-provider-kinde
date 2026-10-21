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
		CheckDestroy:             testAccCheckKindeOrganizationUsersDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationUserRolesConfig(testID, key, "first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.first", "id"),
					testAccCheckKindeOrganizationUserRoles("kinde_organization_user.test", "kinde_role.first"),
				),
			},
			{
				Config: testAccOrganizationUserRolesConfig(testID, key, "second"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.second", "id"),
					testAccCheckKindeOrganizationUserRoles("kinde_organization_user.test", "kinde_role.second"),
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

func TestOrganizationUserResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	first := testAccOrganizationUserRolesConfig("offline", "offline", "first")
	second := testAccOrganizationUserRolesConfig("offline", "offline", "second")
	var membershipID string

	expectRoles := func(roleAddress string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			orgCode, err := stateAttr(state, "kinde_organization_user.test", "organization_code")
			if err != nil {
				return err
			}
			userID, _ := stateAttr(state, "kinde_organization_user.test", "user_id")
			roleID, err := stateAttr(state, roleAddress, "id")
			if err != nil {
				return err
			}
			return fake.check(func() error {
				roles, member := fake.orgMembers[orgCode][userID]
				if !member {
					return fmt.Errorf("user %s is not a member of %s in Kinde", userID, orgCode)
				}
				if len(roles) != 1 || roles[0] != roleID {
					return fmt.Errorf("unexpected roles in Kinde: %v, want [%s]", roles, roleID)
				}
				return nil
			})(state)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if !fake.removedMembers[membershipID] {
				return fmt.Errorf("membership %s was not removed in Kinde on destroy", membershipID)
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					expectRoles("kinde_role.first"),
					captureAttr("kinde_organization_user.test", "id", &membershipID),
				),
			},
			{
				Config: second,
				Check:  expectRoles("kinde_role.second"),
			},
			{
				ResourceName:      "kinde_organization_user.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			offlineNotFoundStep(second, func() {
				fake.do(func() {
					for orgCode := range fake.orgMembers {
						fake.orgMembers[orgCode] = map[string][]string{}
					}
				})
			}),
			{
				Config: second,
				Check:  expectRoles("kinde_role.second"),
			},
		},
	})
}
