// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"testing"

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

func captureAttr(address, attribute string, dst *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		value, err := stateAttr(state, address, attribute)
		*dst = value
		return err
	}
}

func testAccDeleteOutsideTerraform(t *testing.T, what string, remove func(ctx context.Context, client *ManagementAPI) error) func() {
	return func() {
		ctx := context.Background()
		client, err := newManagementClient(ctx, managementConfigFromEnv())
		if err != nil {
			t.Fatalf("failed to create management client: %s", err)
		}
		if err := remove(ctx, client); err != nil {
			t.Fatalf("failed to delete %s outside Terraform: %s", what, err)
		}
	}
}

func testAccCheckKindeAPI(address, name, audience string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		api, err := getKindeAPI(ctx, client, rs.Primary.ID)
		if err != nil {
			return err
		}
		if err := expectEqual("API name", api.Name, name); err != nil {
			return err
		}
		return expectEqual("API audience", api.Audience, audience)
	})
}

func testAccCheckKindeAPIsDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_api", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		_, err := getKindeAPI(ctx, client, rs.Primary.ID)
		if isNotFoundError(err) {
			return false, nil
		}
		return err == nil, err
	})(state)
}

func testAccCheckKindeApplication(address, name, appType string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		app, err := getKindeApplication(ctx, client, rs.Primary.ID)
		if err != nil {
			return err
		}
		if err := expectEqual("application name", app.Name, name); err != nil {
			return err
		}
		if err := expectEqual("application type", app.Type, appType); err != nil {
			return err
		}
		return expectEqual("application client_id", app.ClientID, rs.Primary.Attributes["client_id"])
	})
}

func testAccCheckKindeApplicationsDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_application", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		_, err := getKindeApplication(ctx, client, rs.Primary.ID)
		if isNotFoundError(err) {
			return false, nil
		}
		return err == nil, err
	})(state)
}

func testAccApplicationHasConnection(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
	connections, err := getApplicationConnections(ctx, client, rs.Primary.Attributes["application_id"])
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	for _, connection := range connections {
		if connection.ID == rs.Primary.Attributes["connection_id"] {
			return true, nil
		}
	}
	return false, nil
}

func testAccCheckKindeApplicationConnection(address string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		enabled, err := testAccApplicationHasConnection(ctx, client, rs)
		if err != nil {
			return err
		}
		if !enabled {
			return fmt.Errorf("connection %s is not enabled for application %s in Kinde", rs.Primary.Attributes["connection_id"], rs.Primary.Attributes["application_id"])
		}
		return nil
	})
}

func testAccCheckKindeApplicationConnectionsDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_application_connection", testAccApplicationHasConnection)(state)
}

func testAccCheckKindeOrganization(address, name string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		organization, err := (&OrganizationResource{client: client}).getOrganization(ctx, rs.Primary.Attributes["code"])
		if err != nil {
			return err
		}
		return expectEqual("organization name", organization.Name, name)
	})
}

func testAccCheckKindeOrganizationsDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_organization", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		_, err := (&OrganizationResource{client: client}).getOrganization(ctx, rs.Primary.Attributes["code"])
		if isNotFoundError(err) {
			return false, nil
		}
		return err == nil, err
	})(state)
}

func testAccOrganizationUserRoleIDs(ctx context.Context, client *ManagementAPI, orgCode, userID string) ([]string, bool, error) {
	roles, err := getOrganizationUserRoles(ctx, client, orgCode, userID)
	if err != nil {
		if isNotFoundError(err) || isUserNotInOrganizationError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return ids, true, nil
}

func testAccCheckKindeOrganizationUserRoles(address string, roleAddresses ...string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		var want []string
		for _, roleAddress := range roleAddresses {
			id, err := stateAttr(state, roleAddress, "id")
			if err != nil {
				return err
			}
			want = append(want, id)
		}
		return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
			got, member, err := testAccOrganizationUserRoleIDs(ctx, client, rs.Primary.Attributes["organization_code"], rs.Primary.Attributes["user_id"])
			if err != nil {
				return err
			}
			if !member {
				return fmt.Errorf("user %s is not a member of organization %s in Kinde", rs.Primary.Attributes["user_id"], rs.Primary.Attributes["organization_code"])
			}
			sort.Strings(got)
			sort.Strings(want)
			return expectEqual("organization user roles", fmt.Sprint(got), fmt.Sprint(want))
		})(state)
	}
}

func testAccCheckKindeOrganizationUsersDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_organization_user", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		_, member, err := testAccOrganizationUserRoleIDs(ctx, client, rs.Primary.Attributes["organization_code"], rs.Primary.Attributes["user_id"])
		return member, err
	})(state)
}

func testAccUserHasRole(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
	ids, _, err := testAccOrganizationUserRoleIDs(ctx, client, rs.Primary.Attributes["organization_code"], rs.Primary.Attributes["user_id"])
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == rs.Primary.Attributes["role_id"] {
			return true, nil
		}
	}
	return false, nil
}

func testAccCheckKindeUserRole(address string) resource.TestCheckFunc {
	return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
		assigned, err := testAccUserHasRole(ctx, client, rs)
		if err != nil {
			return err
		}
		if !assigned {
			return fmt.Errorf("role %s is not assigned to user %s in Kinde", rs.Primary.Attributes["role_id"], rs.Primary.Attributes["user_id"])
		}
		return nil
	})
}

func testAccCheckKindeUserRolesDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_user_role", testAccUserHasRole)(state)
}

func testAccCheckKindeRole(address, name, key, description string, permissionAddresses ...string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		want := []string{}
		for _, permissionAddress := range permissionAddresses {
			id, err := stateAttr(state, permissionAddress, "id")
			if err != nil {
				return err
			}
			want = append(want, id)
		}
		return testAccCheckKinde(address, func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) error {
			role, err := (&RoleResource{client: client}).getRole(ctx, rs.Primary.ID)
			if err != nil {
				return err
			}
			if err := expectEqual("role name", role.Name, name); err != nil {
				return err
			}
			if err := expectEqual("role key", role.Key, key); err != nil {
				return err
			}
			if err := expectEqual("role description", role.Description, description); err != nil {
				return err
			}
			got := append([]string{}, role.Permissions...)
			sort.Strings(got)
			sort.Strings(want)
			return expectEqual("role permissions", fmt.Sprint(got), fmt.Sprint(want))
		})(state)
	}
}

func testAccCheckKindeRolesDestroyed(state *terraform.State) error {
	return testAccCheckKindeDestroyed("kinde_role", func(ctx context.Context, client *ManagementAPI, rs *terraform.ResourceState) (bool, error) {
		_, err := (&RoleResource{client: client}).getRole(ctx, rs.Primary.ID)
		if isNotFoundError(err) {
			return false, nil
		}
		return err == nil, err
	})(state)
}
