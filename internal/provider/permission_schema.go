// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

type kindePermission struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type PermissionResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
}

type permissionCreateParams struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Key         string `json:"key"`
}

func expandPermissionCreateParams(d PermissionResourceModel) permissionCreateParams {
	return permissionCreateParams{
		Name:        d.Name.ValueString(),
		Key:         d.Key.ValueString(),
		Description: d.Description.ValueString(),
	}
}

func expandPermissionUpdateParams(d PermissionResourceModel) management_api.UpdatePermissionsReq {
	return management_api.UpdatePermissionsReq{
		Name:        management_api.NewOptString(d.Name.ValueString()),
		Key:         management_api.NewOptString(d.Key.ValueString()),
		Description: management_api.NewOptString(d.Description.ValueString()),
	}
}

func flattenPermissionResource(permission *kindePermission) PermissionResourceModel {
	return PermissionResourceModel{
		ID:          types.StringValue(permission.ID),
		Name:        types.StringValue(permission.Name),
		Key:         types.StringValue(permission.Key),
		Description: types.StringValue(permission.Description),
	}
}

type PermissionDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
}

//nolint:unused
func expandPermissionDataSourceModel(model PermissionDataSourceModel) *kindePermission {
	return &kindePermission{
		ID:          model.ID.ValueString(),
		Name:        model.Name.ValueString(),
		Key:         model.Key.ValueString(),
		Description: model.Description.ValueString(),
	}
}

//nolint:unused
func flattenPermissionDataSource(permission *kindePermission) PermissionDataSourceModel {
	return PermissionDataSourceModel{
		ID:          types.StringValue(permission.ID),
		Name:        types.StringValue(permission.Name),
		Key:         types.StringValue(permission.Key),
		Description: types.StringValue(permission.Description),
	}
}
