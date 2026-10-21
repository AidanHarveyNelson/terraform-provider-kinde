// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ datasource.DataSource = (*APIDataSource)(nil)

func NewAPIDataSource() datasource.DataSource {
	return &APIDataSource{}
}

type APIDataSource struct {
	client *ManagementAPI
}

func (d *APIDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (d *APIDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Computed:            true,
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Computed:            true,
			},
		},
	}
}

func (d *APIDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(*KindeClients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *KindeClients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	client, err := clients.ManagementClient()
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Kinde Management API Client", err.Error())
		return
	}

	d.client = client
}

func (d *APIDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config APIDataSourceModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}

	resource := expandAPIDataSourceModel(config)

	tflog.Debug(ctx, "Reading API", map[string]any{"id": resource.ID})

	resource, err := getKindeAPI(ctx, d.client, resource.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get API", err.Error())
		return
	}

	tflog.Debug(ctx, "Read API", map[string]any{"resource": resource})

	state := flattenAPIDataSource(resource)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
