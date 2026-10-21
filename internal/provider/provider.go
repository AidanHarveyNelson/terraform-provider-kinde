// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go"
)

// Ensure KindeProvider satisfies various provider interfaces.
var _ provider.Provider = &KindeProvider{}

// KindeProvider defines the provider implementation.
type KindeProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

type KindeClients struct {
	*kinde.Client

	Management    *ManagementAPI
	managementErr error
}

func (c *KindeClients) ManagementClient() (*ManagementAPI, error) {
	if c.managementErr != nil {
		return nil, c.managementErr
	}
	return c.Management, nil
}

// KindeProviderModel describes the provider data model.
type KindeProviderModel struct {
	Domain       types.String `tfsdk:"domain"`
	Audience     types.String `tfsdk:"audience"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

func (p *KindeProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kinde"
	resp.Version = p.version
}

func (p *KindeProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "Kinde organisation domain, also set by KINDE_DOMAIN",
				Optional:            true,
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application audience, also set by KINDE_AUDIENCE",
				Optional:            true,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application client id, also set by KINDE_CLIENT_ID",
				Optional:            true,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application client secret, also set by KINDE_CLIENT_SECRET",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *KindeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data KindeProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := kinde.NewClientOptions()

	if !data.Domain.IsNull() && !data.Domain.IsUnknown() {
		if domain := strings.TrimSpace(data.Domain.ValueString()); domain != "" {
			opts.WithDomain(domain)
		}
	}

	if !data.Audience.IsNull() && !data.Audience.IsUnknown() {
		if audience := strings.TrimSpace(data.Audience.ValueString()); audience != "" {
			opts.WithAudience(audience)
		}
	}

	if !data.ClientID.IsNull() && !data.ClientID.IsUnknown() {
		if clientID := strings.TrimSpace(data.ClientID.ValueString()); clientID != "" {
			opts.WithClientID(clientID)
		}
	}

	if !data.ClientSecret.IsNull() && !data.ClientSecret.IsUnknown() {
		if clientSecret := strings.TrimSpace(data.ClientSecret.ValueString()); clientSecret != "" {
			opts.WithClientSecret(clientSecret)
		}
	}

	client := kinde.New(ctx, opts)
	clients := &KindeClients{Client: &client}
	clients.Management, clients.managementErr = newManagementClient(ctx, opts)

	resp.DataSourceData = clients
	resp.ResourceData = clients
}

func (p *KindeProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAPIResource,
		NewApplicationResource,
		NewApplicationConnectionResource,
		NewConnectionResource,
		NewOrganizationResource,
		NewOrganizationUserResource,
		NewRoleResource,
		NewUserResource,
		NewPermissionResource,
		NewUserRoleResource,
		NewEnvironmentFeatureFlagsResource,
	}
}

func (p *KindeProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAPIDataSource,
		NewApplicationDataSource,
		NewConnectionsDataSource,
		NewEnvironmentDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &KindeProvider{
			version: version,
		}
	}
}
