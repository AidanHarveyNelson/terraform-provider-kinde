// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ConnectionsDataSource{}

func NewConnectionsDataSource() datasource.DataSource {
	return &ConnectionsDataSource{}
}

type ConnectionsDataSource struct {
	client *ManagementAPI
}

type ConnectionsDataSourceModel struct {
	Filter      types.String      `tfsdk:"filter"`
	Connections []ConnectionModel `tfsdk:"connections"`
}

type ConnectionModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	DisplayName types.String `tfsdk:"display_name"`
	Strategy    types.String `tfsdk:"strategy"`
}

func (d *ConnectionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connections"
}

func (d *ConnectionsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Use this data source to list available connections.",

		Attributes: map[string]schema.Attribute{
			"filter": schema.StringAttribute{
				MarkdownDescription: "Filter connections by type. Valid values are: `builtin`, `custom`, `all`. Defaults to `all`.",
				Optional:            true,
			},
			"connections": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed: true,
						},
						"name": schema.StringAttribute{
							Computed: true,
						},
						"display_name": schema.StringAttribute{
							Computed: true,
						},
						"strategy": schema.StringAttribute{
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *ConnectionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

const (
	strategyEmailPassword    = "email:password"
	strategyEmailOTP         = "email:otp"
	strategyPhoneOTP         = "phone:otp"
	strategyUsernamePassword = "username:password"
	strategyUsernameOTP      = "username:otp"
)

type kindeConnectionSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Strategy    string `json:"strategy"`
}

func listKindeConnections(ctx context.Context, client *ManagementAPI) ([]kindeConnectionSummary, error) {
	var response struct {
		Connections []kindeConnectionSummary `json:"connections"`
	}
	if err := client.getJSON(ctx, "/api/v1/connections", &response); err != nil {
		return nil, err
	}
	return response.Connections, nil
}

func isBuiltinStrategy(strategy string) bool {
	switch strategy {
	case strategyEmailPassword,
		strategyEmailOTP,
		strategyPhoneOTP,
		strategyUsernamePassword,
		strategyUsernameOTP:
		return true
	default:
		return false
	}
}

func (d *ConnectionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ConnectionsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError(
			"Unconfigured Kinde client",
			"The Kinde provider was not configured before this data source ran. Configure the provider and try again.",
		)
		return
	}

	// Get all connections
	conns, err := listKindeConnections(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read connections, got error: %s", err))
		return
	}

	// Filter connections based on the filter parameter
	filter := "all"
	if !data.Filter.IsNull() {
		filter = data.Filter.ValueString()
	}

	var filteredConns []kindeConnectionSummary
	switch filter {
	case "builtin":
		for _, conn := range conns {
			if isBuiltinStrategy(conn.Strategy) {
				filteredConns = append(filteredConns, conn)
			}
		}
	case "custom":
		for _, conn := range conns {
			if !isBuiltinStrategy(conn.Strategy) {
				filteredConns = append(filteredConns, conn)
			}
		}
	default:
		filteredConns = conns
	}

	// Convert to model
	data.Connections = make([]ConnectionModel, len(filteredConns))
	for i, conn := range filteredConns {
		data.Connections[i] = ConnectionModel{
			ID:          types.StringValue(conn.ID),
			Name:        types.StringValue(conn.Name),
			DisplayName: types.StringValue(conn.DisplayName),
			Strategy:    types.StringValue(conn.Strategy),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
