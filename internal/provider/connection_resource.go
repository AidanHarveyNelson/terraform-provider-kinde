// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/connections"
)

var (
	_ resource.Resource                = &ConnectionResource{}
	_ resource.ResourceWithImportState = &ConnectionResource{}
)

func NewConnectionResource() resource.Resource {
	return &ConnectionResource{}
}

type ConnectionResource struct {
	client *connections.Client
}

// ConnectionOptionsModel represents OAuth2 connection options.
type ConnectionOptionsModel struct {
	ClientID     types.String `tfsdk:"client_id" json:"client_id,omitempty"`
	ClientSecret types.String `tfsdk:"client_secret" json:"client_secret,omitempty"`
}

// IsEmpty returns true if both fields are null or empty.
func (m *ConnectionOptionsModel) IsEmpty() bool {
	if m == nil {
		return true
	}
	// Consider both null and empty string as empty.
	isClientIDEmpty := m.ClientID.IsNull() || m.ClientID.ValueString() == ""
	isClientSecretEmpty := m.ClientSecret.IsNull() || m.ClientSecret.ValueString() == ""
	return isClientIDEmpty && isClientSecretEmpty
}

// Validate ensures both fields are either both set or both null.
func (m *ConnectionOptionsModel) Validate() error {
	if m == nil {
		return nil
	}

	// If either field is set, both must be set
	if (!m.ClientID.IsNull() || !m.ClientSecret.IsNull()) &&
		(m.ClientID.IsNull() || m.ClientSecret.IsNull()) {
		return fmt.Errorf("both client_id and client_secret must be set if either is provided")
	}

	return nil
}

// ToAPIOptions converts the model to API options.
func (m *ConnectionOptionsModel) ToAPIOptions() connections.SocialConnectionOptions {
	if m == nil {
		return connections.SocialConnectionOptions{}
	}

	opts := connections.SocialConnectionOptions{}
	if !m.ClientID.IsNull() {
		opts.ClientID = m.ClientID.ValueString()
	}
	if !m.ClientSecret.IsNull() {
		opts.ClientSecret = m.ClientSecret.ValueString()
	}
	return opts
}

// ConnectionResourceModel represents the resource model.
type ConnectionResourceModel struct {
	ID          types.String            `tfsdk:"id"`
	Name        types.String            `tfsdk:"name"`
	DisplayName types.String            `tfsdk:"display_name"`
	Strategy    types.String            `tfsdk:"strategy"`
	Options     *ConnectionOptionsModel `tfsdk:"options"`
}

// Plan modifier for options.
type optionsEmptyPreserveModifier struct{}

func (m optionsEmptyPreserveModifier) Description(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) MarkdownDescription(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	// If config has a value, use it.
	if !req.ConfigValue.IsNull() {
		resp.PlanValue = req.ConfigValue
		return
	}

	// If state has a value, preserve it.
	if !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}

	// Otherwise, use null.
	resp.PlanValue = req.ConfigValue
}

func (r *ConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *ConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a connection in Kinde.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the connection",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection",
				Required:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Display name of the connection",
				Required:            true,
			},
			"strategy": schema.StringAttribute{
				MarkdownDescription: "Strategy of the connection. Changing this forces a new connection to be created, because the Kinde API does not support updating the strategy of an existing connection.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"options": schema.SingleNestedAttribute{
				MarkdownDescription: `Options for the connection. Required for OAuth2 connections.

**Warning:** Terraform state is not encrypted by default. The ` + "`client_id`" + ` and ` + "`client_secret`" + ` values are stored in state (even when marked as sensitive) and may be written to local state files and remote backends. Use an encrypted remote backend (for example Terraform Cloud/Enterprise, or S3 with ` + "`encrypt = true`" + ` and SSE-KMS).

Example (S3 backend with KMS):

~~~hcl
terraform {
  backend "s3" {
    bucket     = "my-tf-state"
    key        = "kinde/terraform.tfstate"
    region     = "us-east-1"
    encrypt    = true
    kms_key_id = "arn:aws:kms:..."
  }
}
~~~

See: https://developer.hashicorp.com/terraform/language/state/sensitive-data`,
				Optional:      true,
				PlanModifiers: []planmodifier.Object{&optionsEmptyPreserveModifier{}},
				Attributes: map[string]schema.Attribute{
					"client_id": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
					"client_secret": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
				},
			},
		},
	}
}

func (r *ConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*KindeClients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *KindeClients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client.Connections
}

func (r *ConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConnectionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert options to map for API
	var options interface{}
	if plan.Options != nil {
		var err error
		options, err = r.convertOptionsToMap(plan.Strategy.ValueString(), plan.Options)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: %s", err),
			)
			return
		}
	}

	// Create connection
	createParams := connections.CreateParams{
		Name:        plan.Name.ValueString(),
		DisplayName: plan.DisplayName.ValueString(),
		Strategy:    connections.Strategy(plan.Strategy.ValueString()),
		Options:     options,
	}

	conn, err := r.client.Create(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Connection",
			fmt.Sprintf("Could not create connection: %s", err),
		)
		return
	}

	// Set ID from response, keep other fields from plan including options
	plan.ID = types.StringValue(conn.ID)

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	conn, err := r.client.Get(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Connection",
			fmt.Sprintf("Could not read connection ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	// Set basic fields from API response
	state.ID = types.StringValue(conn.ID)
	state.Name = types.StringValue(conn.Name)
	state.DisplayName = types.StringValue(conn.DisplayName)
	state.Strategy = types.StringValue(conn.Strategy)

	// API doesn't return sensitive options, so preserve them from state
	// We're relying on state encryption for security

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert options to map for API
	var options interface{}
	if plan.Options != nil {
		var err error
		options, err = r.convertOptionsToMap(plan.Strategy.ValueString(), plan.Options)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: %s", err),
			)
			return
		}
	}

	// Update connection
	updateParams := connections.UpdateParams{
		Name:        plan.Name.ValueString(),
		DisplayName: plan.DisplayName.ValueString(),
		Options:     options,
	}

	_, err := r.client.Update(ctx, plan.ID.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Connection",
			fmt.Sprintf("Could not update connection ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	diags := resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Connection",
			fmt.Sprintf("Could not delete connection ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *ConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import just the ID, the Read method will handle the rest
	// Note that sensitive options won't be imported and will need to be set in configuration
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)

	// Add a warning about sensitive values
	resp.Diagnostics.AddWarning(
		"Sensitive Values Not Imported",
		"Sensitive connection options like client_id and client_secret cannot be imported and must be set in your configuration. "+
			"After import, you'll need to set these values in your configuration before making any changes that would trigger an update.",
	)
}

func (r *ConnectionResource) convertOptionsToMap(strategy string, options *ConnectionOptionsModel) (interface{}, error) {
	if options == nil {
		return map[string]interface{}{}, nil
	}

	switch connections.Strategy(strategy) {
	case connections.StrategyOAuth2Apple,
		connections.StrategyOAuth2AzureAD,
		connections.StrategyOAuth2Bitbucket,
		connections.StrategyOAuth2Discord,
		connections.StrategyOAuth2Facebook,
		connections.StrategyOAuth2Github,
		connections.StrategyOAuth2Gitlab,
		connections.StrategyOAuth2Google,
		connections.StrategyOAuth2LinkedIn,
		connections.StrategyOAuth2Microsoft,
		connections.StrategyOAuth2Patreon,
		connections.StrategyOAuth2Slack,
		connections.StrategyOAuth2Stripe,
		connections.StrategyOAuth2Twitch,
		connections.StrategyOAuth2Twitter,
		connections.StrategyOAuth2Xero:
		return options.ToAPIOptions(), nil

	default:
		return nil, fmt.Errorf("unsupported strategy: %s", strategy)
	}
}

func (r *ConnectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ConnectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate strategy
	if !data.Strategy.IsNull() {
		strategy := connections.Strategy(data.Strategy.ValueString())
		if strings.HasPrefix(string(strategy), "oauth2:") {
			if data.Options == nil {
				resp.Diagnostics.AddError(
					"Missing OAuth2 Options",
					"options must be set for OAuth2 connections. Use an explicit empty object (`options = {}`) to intentionally clear sensitive values, or provide both client_id and client_secret.",
				)
				return
			}

			if data.Options.IsEmpty() {
				return
			}

			if err := data.Options.Validate(); err != nil {
				resp.Diagnostics.AddError(
					"Invalid Options Configuration",
					err.Error(),
				)
				return
			}
		} else {
			if data.Options != nil && !data.Options.IsEmpty() {
				resp.Diagnostics.AddError(
					"Unsupported Options",
					"options is only supported for OAuth2 connection strategies.",
				)
				return
			}
		}
	}
}
