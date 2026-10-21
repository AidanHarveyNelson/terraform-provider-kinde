// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPermissionResource(t *testing.T) {
	// Permission keys must be unique within a Kinde business.
	suffix := acctest.RandInt()
	name := fmt.Sprintf("tfacc-permission-%d", suffix)
	key := fmt.Sprintf("tfacc_permission_%d", suffix)
	updatedName := fmt.Sprintf("tfacc-permission-updated-%d", suffix)
	updatedKey := fmt.Sprintf("tfacc_permission_updated_%d", suffix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindePermissionsDestroyed,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccPermissionResourceConfig(name, key, "Test permission description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", name),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", key),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Test permission description"),
					resource.TestCheckResourceAttrSet("kinde_permission.test", "id"),
					testAccCheckKindePermission("kinde_permission.test", name, key, "Test permission description"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_permission.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccPermissionResourceConfig(updatedName, updatedKey, "Updated test permission description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", updatedName),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", updatedKey),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Updated test permission description"),
					testAccCheckKindePermission("kinde_permission.test", updatedName, updatedKey, "Updated test permission description"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccPermissionResourceConfig(name string, key string, description string) string {
	return fmt.Sprintf(`
resource "kinde_permission" "test" {
  name        = %[1]q
  key         = %[2]q
  description = %[3]q
}
`, name, key, description)
}

func TestPermissionResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	fake.do(func() {
		for i := 0; i < 150; i++ {
			id := fake.newID("perm")
			fake.permissions[id] = &fakePermission{ID: id, Key: fmt.Sprintf("seed_%d", i), Name: fmt.Sprintf("seed-%d", i)}
		}
	})

	expectPermission := func(name, key, description string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			id, err := stateAttr(state, "kinde_permission.test", "id")
			if err != nil {
				return err
			}
			return fake.check(func() error {
				permission, ok := fake.permissions[id]
				if !ok {
					return fmt.Errorf("permission %s not found in Kinde", id)
				}
				if permission.Name != name || permission.Key != key || permission.Description != description {
					return fmt.Errorf("unexpected permission in Kinde: %+v", *permission)
				}
				return nil
			})(state)
		}
	}

	updated := testAccPermissionResourceConfig("updated", "updated_key", "Updated description")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			for _, permission := range fake.permissions {
				if permission.Key == "updated_key" || permission.Key == "first_key" {
					return fmt.Errorf("permission %s still exists", permission.ID)
				}
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: testAccPermissionResourceConfig("first", "first_key", "First description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "first"),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", "first_key"),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "First description"),
					expectPermission("first", "first_key", "First description"),
				),
			},
			{
				ResourceName:      "kinde_permission.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "updated"),
					expectPermission("updated", "updated_key", "Updated description"),
				),
			},
			offlineNotFoundStep(updated, func() {
				fake.do(func() {
					for id, permission := range fake.permissions {
						if permission.Key == "updated_key" {
							delete(fake.permissions, id)
						}
					}
				})
			}),
			{
				Config: updated,
				Check:  expectPermission("updated", "updated_key", "Updated description"),
			},
		},
	})
}
