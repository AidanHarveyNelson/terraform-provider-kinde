// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type kindeAPI struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Audience        string `json:"audience"`
	IsManagementAPI bool   `json:"is_management_api"`
}

func getKindeAPI(ctx context.Context, client *ManagementAPI, id string) (*kindeAPI, error) {
	var response struct {
		API kindeAPI `json:"api"`
	}
	if err := client.getJSON(ctx, "/api/v1/apis/"+url.PathEscape(id), &response); err != nil {
		return nil, err
	}
	return &response.API, nil
}

type APIResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Audience        types.String `tfsdk:"audience"`
	IsManagementAPI types.Bool   `tfsdk:"is_management_api"`
}

func expandAPIResourceModel(model APIResourceModel) *kindeAPI {
	api := &kindeAPI{
		ID:       model.ID.ValueString(),
		Name:     model.Name.ValueString(),
		Audience: model.Audience.ValueString(),
	}

	if !model.IsManagementAPI.IsNull() && !model.IsManagementAPI.IsUnknown() {
		api.IsManagementAPI = model.IsManagementAPI.ValueBool()
	}

	return api
}

func flattenAPIResource(resource *kindeAPI) APIResourceModel {
	model := APIResourceModel{
		ID:       types.StringValue(resource.ID),
		Name:     types.StringValue(resource.Name),
		Audience: types.StringValue(resource.Audience),
	}

	model.IsManagementAPI = types.BoolValue(resource.IsManagementAPI)

	return model
}

type APIDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Audience types.String `tfsdk:"audience"`
}

func expandAPIDataSourceModel(model APIDataSourceModel) *kindeAPI {
	return &kindeAPI{
		ID:       model.ID.ValueString(),
		Name:     model.Name.ValueString(),
		Audience: model.Audience.ValueString(),
	}
}

func flattenAPIDataSource(resource *kindeAPI) APIDataSourceModel {
	return APIDataSourceModel{
		ID:       types.StringValue(resource.ID),
		Name:     types.StringValue(resource.Name),
		Audience: types.StringValue(resource.Audience),
	}
}
