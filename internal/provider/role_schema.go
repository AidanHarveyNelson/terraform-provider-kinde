// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/kinde-oss/terraform-provider-kinde/internal/serde"
)

type kindeRole struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Key           string   `json:"key"`
	Description   string   `json:"description,omitempty"`
	Permissions   []string `json:"permissions,omitempty"`
	IsDefaultRole bool     `json:"is_default_role"`
}

type kindeRolePermission struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Key         string `json:"key"`
}

type kindeRolePermissionUpdate struct {
	ID        string `json:"id"`
	Operation string `json:"operation,omitempty"`
}

type RoleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
	Permissions types.Set    `tfsdk:"permissions"`
}

//nolint:unused
func expandRoleResourceModel(data RoleResourceModel) kindeRole {
	return kindeRole{
		ID:          data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Key:         data.Key.ValueString(),
		Description: data.Description.ValueString(),
	}
}

func expandRoleCreateParams(plan RoleResourceModel) management_api.CreateRoleReq {
	return management_api.CreateRoleReq{
		Name:        management_api.NewOptString(plan.Name.ValueString()),
		Key:         management_api.NewOptString(plan.Key.ValueString()),
		Description: optStringIfSet(plan.Description.ValueString()),
	}
}

func expandRoleUpdateParams(plan RoleResourceModel) management_api.UpdateRolesReq {
	return management_api.UpdateRolesReq{
		Name:        plan.Name.ValueString(),
		Key:         plan.Key.ValueString(),
		Description: optStringIfSet(plan.Description.ValueString()),
	}
}

func normalizeRolePermissions(permissions []string) []string {
	if len(permissions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(permissions))
	normalized := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		if permission == "" {
			continue
		}
		if _, exists := seen[permission]; exists {
			continue
		}
		seen[permission] = struct{}{}
		normalized = append(normalized, permission)
	}

	if len(normalized) == 0 {
		return nil
	}

	sort.Strings(normalized)

	return normalized
}

func flattenRoleResource(ctx context.Context, role *kindeRole, permissions []string) (RoleResourceModel, error) {
	permissions = normalizeRolePermissions(permissions)

	var permissionsSet types.Set
	if len(permissions) > 0 {
		var diags diag.Diagnostics
		permissionsSet, diags = types.SetValueFrom(ctx, types.StringType, permissions)
		if diags.HasError() {
			return RoleResourceModel{}, fmt.Errorf("failed to flatten permissions: %v", diags)
		}
	} else {
		permissionsSet = types.SetNull(types.StringType)
	}

	return RoleResourceModel{
		ID:          types.StringValue(role.ID),
		Name:        types.StringValue(role.Name),
		Key:         types.StringValue(role.Key),
		Description: types.StringValue(role.Description),
		Permissions: permissionsSet,
	}, nil
}

type RoleDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
	Permissions types.List   `tfsdk:"permissions"`
}

//nolint:unused
func expandRoleDataSourceModel(model RoleDataSourceModel) *kindeRole {
	return &kindeRole{
		ID:          model.ID.ValueString(),
		Name:        model.Name.ValueString(),
		Key:         model.Key.ValueString(),
		Description: model.Description.ValueString(),
	}
}

//nolint:unused
func flattenRoleDataSource(ctx context.Context, resource *kindeRole, permissions []string) (RoleDataSourceModel, error) {
	permissionsList, diags := serde.FlattenStringList(ctx, normalizeRolePermissions(permissions))
	if diags.HasError() {
		return RoleDataSourceModel{}, fmt.Errorf("failed to flatten permissions: %v", diags)
	}

	return RoleDataSourceModel{
		ID:          types.StringValue(resource.ID),
		Name:        types.StringValue(resource.Name),
		Key:         types.StringValue(resource.Key),
		Description: types.StringValue(resource.Description),
		Permissions: permissionsList,
	}, nil
}
