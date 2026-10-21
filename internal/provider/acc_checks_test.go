// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCheckKinde(address string, check func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not found in state", address)
		}

		ctx := context.Background()
		client, err := newManagementClient(ctx, managementConfigFromEnv())
		if err != nil {
			return err
		}
		return check(ctx, client, rs)
	}
}

func testAccCheckKindeDestroyed(resourceType string, exists func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error)) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		ctx := context.Background()
		client, err := newManagementClient(ctx, managementConfigFromEnv())
		if err != nil {
			return err
		}

		for address, rs := range state.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			found, err := exists(ctx, client, rs)
			if err != nil {
				return fmt.Errorf("checking %s was destroyed: %w", address, err)
			}
			if found {
				return fmt.Errorf("%s (%s) still exists in Kinde", address, rs.Primary.ID)
			}
		}
		return nil
	}
}

func expectEqual(what, got, want string) error {
	if got != want {
		return fmt.Errorf("%s in Kinde is %q, want %q", what, got, want)
	}
	return nil
}

func testAccFindPermission(ctx context.Context, client *ManagementAPI, id string) (*kindePermission, error) {
	permissions, err := (&PermissionResource{client: client}).listAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	for _, permission := range permissions {
		if permission.ID == id {
			return &permission, nil
		}
	}
	return nil, nil
}

func testAccCheckKindePermission(address, name, key, description string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		permission, err := testAccFindPermission(ctx, client, rs.Primary.ID)
		if err != nil {
			return err
		}
		if permission == nil {
			return fmt.Errorf("permission %s not found in Kinde", rs.Primary.ID)
		}
		if err := expectEqual("permission name", permission.Name, name); err != nil {
			return err
		}
		if err := expectEqual("permission key", permission.Key, key); err != nil {
			return err
		}
		return expectEqual("permission description", permission.Description, description)
	})
}

func testAccCheckKindePermissionsDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_permission", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		permission, err := testAccFindPermission(ctx, client, rs.Primary.ID)
		return permission != nil, err
	})(state)
}
