// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

const environmentFeatureFlagsID = "environment"

var (
	_ resource.Resource                = &EnvironmentFeatureFlagsResource{}
	_ resource.ResourceWithImportState = &EnvironmentFeatureFlagsResource{}
)

func NewEnvironmentFeatureFlagsResource() resource.Resource {
	return &EnvironmentFeatureFlagsResource{}
}

type EnvironmentFeatureFlagsResource struct {
	client *ManagementAPI
}

type EnvironmentFeatureFlagsResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Flags types.Map    `tfsdk:"flags"`
}

func (r *EnvironmentFeatureFlagsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment_feature_flags"
}

func (r *EnvironmentFeatureFlagsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Overrides feature flag values for the environment the provider is authenticated against. " +
			"Only the flags listed in `flags` are managed; overrides set outside Terraform on other flags are left alone. " +
			"Declare this resource once per environment. " +
			"See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/environments) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Always `environment`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"flags": schema.MapAttribute{
				MarkdownDescription: "Override values keyed by feature flag key. Values are strings in the form Kinde returns them, " +
					"e.g. `\"true\"` or `\"false\"` for boolean flags and `\"42\"` for integer flags. The flags must already exist.",
				ElementType: types.StringType,
				Required:    true,
			},
		},
	}
}

func (r *EnvironmentFeatureFlagsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *EnvironmentFeatureFlagsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan EnvironmentFeatureFlagsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desired, diags := flagsFromModel(ctx, plan.Flags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	applied, err := r.applyFlags(ctx, map[string]string{}, desired)
	if err != nil {
		if _, cleanupErr := r.applyFlags(ctx, applied, map[string]string{}); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"Error Cleaning Up Environment Feature Flag Overrides",
				fmt.Sprintf("Could not remove overrides applied before the create failed. Remove them in Kinde: %s", cleanupErr),
			)
		}
		resp.Diagnostics.AddError("Error Creating Environment Feature Flag Overrides", err.Error())
		return
	}

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, applied)...)
}

func (r *EnvironmentFeatureFlagsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state EnvironmentFeatureFlagsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	managed, diags := flagsFromModel(ctx, state.Flags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	current, err := r.listFlags(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Environment Feature Flags", err.Error())
		return
	}

	refreshed := make(map[string]string, len(managed))
	for key, previous := range managed {
		value, ok := current[key]
		if !ok {
			continue
		}
		if sameFeatureFlagValue(previous, value) {
			value = previous
		}
		refreshed[key] = value
	}

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, refreshed)...)
}

func (r *EnvironmentFeatureFlagsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state EnvironmentFeatureFlagsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desired, diags := flagsFromModel(ctx, plan.Flags)
	resp.Diagnostics.Append(diags...)
	previous, diags := flagsFromModel(ctx, state.Flags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	applied, err := r.applyFlags(ctx, previous, desired)
	resp.Diagnostics.Append(r.setState(ctx, &resp.State, applied)...)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Environment Feature Flag Overrides", err.Error())
	}
}

func (r *EnvironmentFeatureFlagsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state EnvironmentFeatureFlagsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	managed, diags := flagsFromModel(ctx, state.Flags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.applyFlags(ctx, managed, map[string]string{}); err != nil {
		resp.Diagnostics.AddError("Error Deleting Environment Feature Flag Overrides", err.Error())
	}
}

func (r *EnvironmentFeatureFlagsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	flags := map[string]string{}
	for _, key := range strings.Split(req.ID, ",") {
		if key = strings.TrimSpace(key); key != "" {
			flags[key] = ""
		}
	}

	if len(flags) == 0 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Expected a comma-separated list of feature flag keys, e.g. \"dark_mode,max_seats\".",
		)
		return
	}

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, flags)...)
}

func (r *EnvironmentFeatureFlagsResource) applyFlags(ctx context.Context, previous, desired map[string]string) (map[string]string, error) {
	applied := make(map[string]string, len(previous))
	for key, value := range previous {
		applied[key] = value
	}

	for _, key := range sortedKeys(previous) {
		if _, keep := desired[key]; keep {
			continue
		}
		if err := r.deleteOverride(ctx, key); err != nil {
			return applied, err
		}
		delete(applied, key)
	}

	for _, key := range sortedKeys(desired) {
		value := desired[key]
		if current, ok := previous[key]; ok && current == value {
			continue
		}
		if err := r.setOverride(ctx, key, value); err != nil {
			return applied, err
		}
		applied[key] = value
	}

	return applied, nil
}

func (r *EnvironmentFeatureFlagsResource) listFlags(ctx context.Context) (map[string]string, error) {
	var body struct {
		FeatureFlags map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"feature_flags"`
	}
	if err := r.client.getJSON(ctx, "/api/v1/environment/feature_flags", &body); err != nil {
		return nil, fmt.Errorf("could not list environment feature flags: %w", err)
	}

	flags := make(map[string]string, len(body.FeatureFlags))
	for key, flag := range body.FeatureFlags {
		value, err := featureFlagValueString(flag.Value)
		if err != nil {
			return nil, fmt.Errorf("could not read value of feature flag %q: %w", key, err)
		}
		flags[key] = value
	}
	return flags, nil
}

func featureFlagValueString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, nil
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}

	if number, ok := value.(json.Number); ok {
		return number.String(), nil
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func (r *EnvironmentFeatureFlagsResource) setOverride(ctx context.Context, key, value string) error {
	res, err := r.client.UpdateEnvironementFeatureFlagOverride(ctx,
		&management_api.UpdateEnvironementFeatureFlagOverrideReq{Value: value},
		management_api.UpdateEnvironementFeatureFlagOverrideParams{FeatureFlagKey: key},
	)
	if err != nil {
		return fmt.Errorf("could not set override for feature flag %q: %w", key, err)
	}
	if _, ok := res.(*management_api.SuccessResponse); !ok {
		return managementAPIError(fmt.Sprintf("set override for feature flag %q", key), res)
	}
	return nil
}

func (r *EnvironmentFeatureFlagsResource) deleteOverride(ctx context.Context, key string) error {
	res, err := r.client.DeleteEnvironementFeatureFlagOverride(ctx,
		management_api.DeleteEnvironementFeatureFlagOverrideParams{FeatureFlagKey: key},
	)
	if err != nil {
		return fmt.Errorf("could not delete override for feature flag %q: %w", key, err)
	}
	if _, ok := res.(*management_api.SuccessResponse); !ok {
		return managementAPIError(fmt.Sprintf("delete override for feature flag %q", key), res)
	}
	return nil
}

func (r *EnvironmentFeatureFlagsResource) setState(ctx context.Context, state *tfsdk.State, flags map[string]string) diag.Diagnostics {
	flagsValue, diags := types.MapValueFrom(ctx, types.StringType, flags)
	if diags.HasError() {
		return diags
	}

	diags.Append(state.Set(ctx, &EnvironmentFeatureFlagsResourceModel{
		ID:    types.StringValue(environmentFeatureFlagsID),
		Flags: flagsValue,
	})...)
	return diags
}

func flagsFromModel(ctx context.Context, value types.Map) (map[string]string, diag.Diagnostics) {
	flags := map[string]string{}
	if value.IsNull() || value.IsUnknown() {
		return flags, nil
	}
	diags := value.ElementsAs(ctx, &flags, false)
	return flags, diags
}

func sameFeatureFlagValue(a, b string) bool {
	if a == b {
		return true
	}
	canonicalA, errA := featureFlagValueString(json.RawMessage(a))
	canonicalB, errB := featureFlagValueString(json.RawMessage(b))
	return errA == nil && errB == nil && canonicalA == canonicalB && strings.TrimSpace(a) != ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
