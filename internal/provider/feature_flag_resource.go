// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	featureFlagTypes          = []string{"str", "int", "bool"}
	featureFlagOverrideLevels = []string{"env", "org", "usr"}
)

var (
	_ resource.Resource                   = &FeatureFlagResource{}
	_ resource.ResourceWithImportState    = &FeatureFlagResource{}
	_ resource.ResourceWithValidateConfig = &FeatureFlagResource{}
)

func NewFeatureFlagResource() resource.Resource {
	return &FeatureFlagResource{}
}

type FeatureFlagResource struct {
	client *ManagementAPI
}

type FeatureFlagResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Key                types.String `tfsdk:"key"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Type               types.String `tfsdk:"type"`
	AllowOverrideLevel types.String `tfsdk:"allow_override_level"`
	DefaultValue       types.String `tfsdk:"default_value"`
}

func (r *FeatureFlagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_feature_flag"
}

func (r *FeatureFlagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Feature flags in the environment the provider is authenticated against. " +
			"Kinde can't return a single flag, so `name`, `description` and `allow_override_level` are not refreshed from Kinde, " +
			"and `default_value` is only refreshed for flags that can't be overridden at environment level. " +
			"See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/feature-flags) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The flag's key.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The flag identifier to use in code. Changing it replaces the flag.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the flag.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the flag's purpose.",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The value type: `str`, `int` or `bool`.",
				Required:            true,
			},
			"allow_override_level": schema.StringAttribute{
				MarkdownDescription: "The one level the flag can be overridden at: `env` (environment), `org` (organization) or `usr` (user). " +
					"Overrides at any other level are rejected by Kinde.",
				Required: true,
			},
			"default_value": schema.StringAttribute{
				MarkdownDescription: "The default value, as a string: `\"true\"` or `\"false\"` for `bool` flags and a whole number for `int` flags.",
				Required:            true,
			},
		},
	}
}

func (r *FeatureFlagResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data FeatureFlagResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.Type.IsNull() && !data.Type.IsUnknown() && !slices.Contains(featureFlagTypes, data.Type.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Invalid Feature Flag Type",
			fmt.Sprintf("type must be one of %s.", strings.Join(featureFlagTypes, ", ")))
	}

	if !data.AllowOverrideLevel.IsNull() && !data.AllowOverrideLevel.IsUnknown() && !slices.Contains(featureFlagOverrideLevels, data.AllowOverrideLevel.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("allow_override_level"), "Invalid Override Level",
			fmt.Sprintf("allow_override_level must be one of %s.", strings.Join(featureFlagOverrideLevels, ", ")))
	}

	if data.Type.IsNull() || data.Type.IsUnknown() || data.DefaultValue.IsNull() || data.DefaultValue.IsUnknown() {
		return
	}

	value := data.DefaultValue.ValueString()
	switch data.Type.ValueString() {
	case "bool":
		if value != "true" && value != "false" {
			resp.Diagnostics.AddAttributeError(path.Root("default_value"), "Invalid Default Value",
				"default_value must be \"true\" or \"false\" for bool flags.")
		}
	case "int":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("default_value"), "Invalid Default Value",
				"default_value must be a whole number for int flags.")
		}
	}
}

func (r *FeatureFlagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(*KindeClients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *KindeClients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	client, err := clients.ManagementClient()
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Kinde Management API Client", err.Error())
		return
	}

	r.client = client
}

func (r *FeatureFlagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FeatureFlagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := &management_api.CreateFeatureFlagReq{
		Name:               plan.Name.ValueString(),
		Key:                plan.Key.ValueString(),
		Type:               management_api.CreateFeatureFlagReqType(plan.Type.ValueString()),
		AllowOverrideLevel: management_api.NewOptCreateFeatureFlagReqAllowOverrideLevel(management_api.CreateFeatureFlagReqAllowOverrideLevel(plan.AllowOverrideLevel.ValueString())),
		DefaultValue:       plan.DefaultValue.ValueString(),
	}
	if !plan.Description.IsNull() {
		params.Description = management_api.NewOptString(plan.Description.ValueString())
	}

	res, err := r.client.CreateFeatureFlag(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Feature Flag", fmt.Sprintf("Could not create feature flag %q: %s", plan.Key.ValueString(), err))
		return
	}
	if _, ok := res.(*management_api.SuccessResponse); !ok {
		resp.Diagnostics.AddError("Error Creating Feature Flag", managementAPIError(fmt.Sprintf("create feature flag %q", plan.Key.ValueString()), res).Error())
		return
	}

	plan.ID = plan.Key
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FeatureFlagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FeatureFlagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	flags, err := r.listFlags(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Feature Flag", err.Error())
		return
	}

	flag, ok := flags[state.Key.ValueString()]
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = state.Key
	if flag.Type != "" {
		state.Type = types.StringValue(flag.Type)
	}
	if state.AllowOverrideLevel.ValueString() != "env" && !sameFeatureFlagValue(state.DefaultValue.ValueString(), flag.Value) {
		state.DefaultValue = types.StringValue(flag.Value)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FeatureFlagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FeatureFlagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	query := url.Values{}
	query.Set("key", plan.Key.ValueString())
	query.Set("name", plan.Name.ValueString())
	query.Set("description", plan.Description.ValueString())
	query.Set("type", plan.Type.ValueString())
	query.Set("allow_override_level", plan.AllowOverrideLevel.ValueString())
	query.Set("default_value", plan.DefaultValue.ValueString())

	endpoint := "/api/v1/feature_flags/" + url.PathEscape(plan.Key.ValueString())
	if err := r.client.requestJSON(ctx, http.MethodPut, endpoint, query, nil); err != nil {
		resp.Diagnostics.AddError("Error Updating Feature Flag", fmt.Sprintf("Could not update feature flag %q: %s", plan.Key.ValueString(), err))
		return
	}

	plan.ID = plan.Key
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FeatureFlagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FeatureFlagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := deleteFeatureFlag(ctx, r.client, state.Key.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error Deleting Feature Flag", err.Error())
	}
}

func (r *FeatureFlagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key := strings.TrimSpace(req.ID)
	if key == "" {
		resp.Diagnostics.AddError("Invalid Import ID", "Expected the feature flag key.")
		return
	}

	flags, err := r.listFlags(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Feature Flag", err.Error())
		return
	}

	flag, ok := flags[key]
	if !ok {
		resp.Diagnostics.AddError("Error Importing Feature Flag", fmt.Sprintf("Feature flag %q was not found.", key))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &FeatureFlagResourceModel{
		ID:                 types.StringValue(key),
		Key:                types.StringValue(key),
		Name:               types.StringNull(),
		Description:        types.StringNull(),
		Type:               types.StringValue(flag.Type),
		AllowOverrideLevel: types.StringNull(),
		DefaultValue:       types.StringValue(flag.Value),
	})...)
}

type featureFlagState struct {
	Type  string
	Value string
}

func (r *FeatureFlagResource) listFlags(ctx context.Context) (map[string]featureFlagState, error) {
	var body struct {
		FeatureFlags map[string]struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"feature_flags"`
	}
	if err := r.client.getJSON(ctx, "/api/v1/environment/feature_flags", &body); err != nil {
		return nil, fmt.Errorf("could not list feature flags: %w", err)
	}

	flags := make(map[string]featureFlagState, len(body.FeatureFlags))
	for key, flag := range body.FeatureFlags {
		value, err := featureFlagValueString(flag.Value)
		if err != nil {
			return nil, fmt.Errorf("could not read value of feature flag %q: %w", key, err)
		}
		flags[key] = featureFlagState{Type: flag.Type, Value: value}
	}
	return flags, nil
}

func deleteFeatureFlag(ctx context.Context, client *ManagementAPI, key string) error {
	res, err := client.DeleteFeatureFlag(ctx, management_api.DeleteFeatureFlagParams{FeatureFlagKey: key})
	if err != nil {
		return fmt.Errorf("could not delete feature flag %q: %w", key, err)
	}
	if _, ok := res.(*management_api.SuccessResponse); ok {
		return nil
	}

	apiErr := managementAPIError(fmt.Sprintf("delete feature flag %q", key), res)
	if strings.Contains(apiErr.Error(), "FEATURE_FLAG_NOT_FOUND") {
		return nil
	}
	return apiErr
}
