// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccRoleResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	var roleID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeRolesDestroyed,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testAccRoleResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role"),
					resource.TestCheckResourceAttrSet("kinde_role.test", "id"),
					testAccCheckKindeRole("kinde_role.test", testID, testID, "Test role"),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "kinde_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing.
			{
				Config: testAccRoleResourceConfigUpdate(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID+"-updated"),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Updated test role"),
					testAccCheckKindeRole("kinde_role.test", testID+"-updated", testID, "Updated test role"),
					captureAttr("kinde_role.test", "id", &roleID),
				),
			},
			{
				PreConfig: testAccDeleteOutsideTerraform(t, "role", func(ctx context.Context, client *ManagementAPI) error {
					return (&RoleResource{client: client}).deleteRole(ctx, roleID)
				}),
				Config:             testAccRoleResourceConfigUpdate(testID),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccRoleResourceConfigUpdate(testID),
				Check:  testAccCheckKindeRole("kinde_role.test", testID+"-updated", testID, "Updated test role"),
			},
		},
	})
}

func TestAccRoleResource_PermissionOrdering(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")
	permission1ID := acctest.RandomWithPrefix("tfacc-perm1")
	permission2ID := acctest.RandomWithPrefix("tfacc-perm2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeRolesDestroyed,
		Steps: []resource.TestStep{
			// Create both permissions and the role with permissions in one order.
			{
				Config: testAccRoleWithPermissionResourcesConfig(testID, permission1ID, permission2ID, []string{"kinde_permission.perm1.id", "kinde_permission.perm2.id"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role with permissions"),
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "2"),
					testAccCheckKindeRole("kinde_role.test", testID, testID, "Test role with permissions", "kinde_permission.perm1", "kinde_permission.perm2"),
				),
			},
			// Update with same permissions in different order - should not trigger a change.
			{
				Config:   testAccRoleWithPermissionResourcesConfig(testID, permission1ID, permission2ID, []string{"kinde_permission.perm2.id", "kinde_permission.perm1.id"}),
				PlanOnly: true,
			},
			// Remove all permissions.
			{
				Config: testAccRoleResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role"),
					resource.TestCheckNoResourceAttr("kinde_role.test", "permissions"),
					testAccCheckKindeRole("kinde_role.test", testID, testID, "Test role"),
				),
			},
		},
	})
}

func TestAccRoleResource_RemovePermissions(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")
	permission1ID := acctest.RandomWithPrefix("tfacc-perm1")
	permission2ID := acctest.RandomWithPrefix("tfacc-perm2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create both permissions and the role with two permissions.
			{
				Config: testAccRoleWithPermissionResourcesConfig(testID, permission1ID, permission2ID, []string{"kinde_permission.perm1.id", "kinde_permission.perm2.id"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role with permissions"),
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "2"),
				),
			},
			// Remove one permission.
			{
				Config: testAccRoleWithPermissionResourcesConfig(testID, permission1ID, permission2ID, []string{"kinde_permission.perm1.id"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role with permissions"),
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "1"),
					testAccCheckKindeRole("kinde_role.test", testID, testID, "Test role with permissions", "kinde_permission.perm1"),
				),
			},
			// Remove all permissions.
			{
				Config: testAccRoleResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role"),
					resource.TestCheckNoResourceAttr("kinde_role.test", "permissions"),
					testAccCheckKindeRole("kinde_role.test", testID, testID, "Test role"),
				),
			},
		},
	})
}

func testAccRoleResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = %[1]q
	key         = %[1]q
	description = "Test role"
}
`, name)
}

func testAccRoleResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = "%[1]s-updated"
	key         = %[1]q
	description = "Updated test role"
}
`, name)
}

func testAccRoleWithPermissionResourcesConfig(roleName, permission1Key, permission2Key string, permissionRefs []string) string {
	permissionsStr := "[]"
	if len(permissionRefs) > 0 {
		permissionsStr = "[" + strings.Join(permissionRefs, ", ") + "]"
	}

	return fmt.Sprintf(`
resource "kinde_permission" "perm1" {
  name        = "test-permission-1"
  key         = %q
  description = "Test permission 1"
}

resource "kinde_permission" "perm2" {
  name        = "test-permission-2"
  key         = %q
  description = "Test permission 2"
}

resource "kinde_role" "test" {
  name        = %q
  key         = %q
  description = "Test role with permissions"
  permissions = %s
}
`, permission1Key, permission2Key, roleName, roleName, permissionsStr)
}

func TestRoleResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	both := testAccRoleWithPermissionResourcesConfig("role", "perm_one", "perm_two", []string{"kinde_permission.perm1.id", "kinde_permission.perm2.id"})
	reordered := testAccRoleWithPermissionResourcesConfig("role", "perm_one", "perm_two", []string{"kinde_permission.perm2.id", "kinde_permission.perm1.id"})
	one := testAccRoleWithPermissionResourcesConfig("role", "perm_one", "perm_two", []string{"kinde_permission.perm2.id"})
	none := testAccRoleResourceConfigUpdate("role")
	var roleID string

	expectRole := func(name, description string, permissionAddresses ...string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			id, err := stateAttr(state, "kinde_role.test", "id")
			if err != nil {
				return err
			}
			var want []string
			for _, permissionAddress := range permissionAddresses {
				permissionID, err := stateAttr(state, permissionAddress, "id")
				if err != nil {
					return err
				}
				want = append(want, permissionID)
			}
			return fake.check(func() error {
				role, ok := fake.roles[id]
				if !ok {
					return fmt.Errorf("role %s not found in Kinde", id)
				}
				got := append([]string{}, role.Permissions...)
				sort.Strings(got)
				sort.Strings(want)
				if role.Name != name || role.Key != "role" || role.Description != description || fmt.Sprint(got) != fmt.Sprint(want) {
					return fmt.Errorf("unexpected role in Kinde: %+v with permissions %v, want %v", *role, got, want)
				}
				return nil
			})(state)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if _, ok := fake.roles[roleID]; ok {
				return fmt.Errorf("role %s still exists", roleID)
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: both,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "2"),
					expectRole("role", "Test role with permissions", "kinde_permission.perm1", "kinde_permission.perm2"),
				),
			},
			{
				Config:   reordered,
				PlanOnly: true,
			},
			{
				ResourceName:      "kinde_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: one,
				Check:  expectRole("role", "Test role with permissions", "kinde_permission.perm2"),
			},
			{
				Config: none,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("kinde_role.test", "permissions"),
					expectRole("role-updated", "Updated test role"),
					captureAttr("kinde_role.test", "id", &roleID),
				),
			},
			offlineNotFoundStep(none, func() {
				fake.do(func() { delete(fake.roles, roleID) })
			}),
			{
				Config: none,
				Check: resource.ComposeAggregateTestCheckFunc(
					expectRole("role-updated", "Updated test role"),
					captureAttr("kinde_role.test", "id", &roleID),
				),
			},
		},
	})
}
