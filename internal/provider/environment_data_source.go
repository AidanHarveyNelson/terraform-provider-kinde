// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var _ datasource.DataSource = &EnvironmentDataSource{}

func NewEnvironmentDataSource() datasource.DataSource {
	return &EnvironmentDataSource{}
}

type EnvironmentDataSource struct {
	client *ManagementAPI
}

type EnvironmentDataSourceModel struct {
	Code               types.String `tfsdk:"code"`
	Name               types.String `tfsdk:"name"`
	IsDefault          types.Bool   `tfsdk:"is_default"`
	IsLive             types.Bool   `tfsdk:"is_live"`
	KindeDomain        types.String `tfsdk:"kinde_domain"`
	CustomDomain       types.String `tfsdk:"custom_domain"`
	CreatedOn          types.String `tfsdk:"created_on"`
	ThemeCode          types.String `tfsdk:"theme_code"`
	ColorScheme        types.String `tfsdk:"color_scheme"`
	Logo               types.String `tfsdk:"logo"`
	LogoDark           types.String `tfsdk:"logo_dark"`
	FaviconSvg         types.String `tfsdk:"favicon_svg"`
	FaviconFallback    types.String `tfsdk:"favicon_fallback"`
	HotjarSiteID       types.String `tfsdk:"hotjar_site_id"`
	GoogleAnalyticsTag types.String `tfsdk:"google_analytics_tag"`
	ContentsquareTagID types.String `tfsdk:"contentsquare_tag_id"`
}

func (d *EnvironmentDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *EnvironmentDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the Kinde environment the provider's M2M application belongs to. Environments cannot be created through the Management API, so this is read-only.",
		Attributes: map[string]schema.Attribute{
			"code": schema.StringAttribute{
				Description: "The unique identifier of the environment, e.g. `production`.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "The name of the environment.",
				Computed:    true,
			},
			"is_default": schema.BoolAttribute{
				Description: "Whether this is the default environment, typically production.",
				Computed:    true,
			},
			"is_live": schema.BoolAttribute{
				Description: "Whether the environment is live.",
				Computed:    true,
			},
			"kinde_domain": schema.StringAttribute{
				Description: "The environment's domain on Kinde.",
				Computed:    true,
			},
			"custom_domain": schema.StringAttribute{
				Description: "The environment's custom domain, if one is set.",
				Computed:    true,
			},
			"created_on": schema.StringAttribute{
				Description: "When the environment was created, in ISO 8601 format.",
				Computed:    true,
			},
			"theme_code": schema.StringAttribute{
				Description: "Whether the environment is forced into `light` or `dark` mode, or follows `user_preference`.",
				Computed:    true,
			},
			"color_scheme": schema.StringAttribute{
				Description: "The color scheme used for meta tags, based on the theme code.",
				Computed:    true,
			},
			"logo": schema.StringAttribute{
				Description: "The environment's logo URL.",
				Computed:    true,
			},
			"logo_dark": schema.StringAttribute{
				Description: "The environment's logo URL for dark themes.",
				Computed:    true,
			},
			"favicon_svg": schema.StringAttribute{
				Description: "The environment's SVG favicon URL.",
				Computed:    true,
			},
			"favicon_fallback": schema.StringAttribute{
				Description: "The environment's fallback (PNG) favicon URL.",
				Computed:    true,
			},
			"hotjar_site_id": schema.StringAttribute{
				Description: "The environment's Hotjar site ID.",
				Computed:    true,
			},
			"google_analytics_tag": schema.StringAttribute{
				Description: "The environment's Google Analytics tag.",
				Computed:    true,
			},
			"contentsquare_tag_id": schema.StringAttribute{
				Description: "The environment's Contentsquare tag ID.",
				Computed:    true,
			},
		},
	}
}

func (d *EnvironmentDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	res, err := d.client.GetEnvironment(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Environment", fmt.Sprintf("Could not read environment: %s", err))
		return
	}

	body, ok := res.(*management_api.GetEnvironmentResponse)
	if !ok {
		resp.Diagnostics.AddError("Error Reading Environment", managementAPIError("get environment", res).Error())
		return
	}

	env, ok := body.Environment.Get()
	if !ok {
		resp.Diagnostics.AddError("Error Reading Environment", "The Kinde API returned no environment.")
		return
	}

	state := EnvironmentDataSourceModel{
		Code:               optStringValue(env.Code),
		Name:               optStringValue(env.Name),
		IsDefault:          optBoolValue(env.IsDefault),
		IsLive:             optBoolValue(env.IsLive),
		KindeDomain:        optStringValue(env.KindeDomain),
		CustomDomain:       optNilStringValue(env.CustomDomain),
		CreatedOn:          optStringValue(env.CreatedOn),
		Logo:               optNilStringValue(env.Logo),
		LogoDark:           optNilStringValue(env.LogoDark),
		FaviconSvg:         optNilStringValue(env.FaviconSvg),
		FaviconFallback:    optNilStringValue(env.FaviconFallback),
		HotjarSiteID:       optNilStringValue(env.HotjarSiteID),
		GoogleAnalyticsTag: optNilStringValue(env.GoogleAnalyticsTag),
		ContentsquareTagID: optNilStringValue(env.ContentsquareTagID),
		ThemeCode:          types.StringNull(),
		ColorScheme:        types.StringNull(),
	}
	if themeCode, ok := env.ThemeCode.Get(); ok {
		state.ThemeCode = types.StringValue(string(themeCode))
	}
	if colorScheme, ok := env.ColorScheme.Get(); ok {
		state.ColorScheme = types.StringValue(string(colorScheme))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func optStringValue(v management_api.OptString) types.String {
	if value, ok := v.Get(); ok {
		return types.StringValue(value)
	}
	return types.StringNull()
}

func optNilStringValue(v management_api.OptNilString) types.String {
	if value, ok := v.Get(); ok {
		return types.StringValue(value)
	}
	return types.StringNull()
}

func optBoolValue(v management_api.OptBool) types.Bool {
	if value, ok := v.Get(); ok {
		return types.BoolValue(value)
	}
	return types.BoolNull()
}
