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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type featureFlagOverrideScope struct {
	typeName         string
	label            string
	description      string
	ownerAttribute   string
	ownerDescription string
	fixedID          string
	removable        bool
	listPath         func(owner string) string
	setOverride      func(ctx context.Context, client *ManagementAPI, owner, key, value string) error
	deleteOverride   func(ctx context.Context, client *ManagementAPI, owner, key string) error
}

var (
	_ resource.Resource                = &featureFlagOverridesResource{}
	_ resource.ResourceWithImportState = &featureFlagOverridesResource{}
)

type featureFlagOverridesResource struct {
	scope  featureFlagOverrideScope
	client *ManagementAPI
}

func (r *featureFlagOverridesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.scope.typeName
}

func (r *featureFlagOverridesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	idDescription := fmt.Sprintf("Always `%s`.", r.scope.fixedID)
	if r.scope.ownerAttribute != "" {
		idDescription = fmt.Sprintf("The `%s`.", r.scope.ownerAttribute)
	}

	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: idDescription,
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"flags": schema.MapAttribute{
			MarkdownDescription: "Override values keyed by feature flag key. Values are strings in the form Kinde returns them, " +
				"e.g. `\"true\"` or `\"false\"` for boolean flags and `\"42\"` for integer flags. The flags must already exist.",
			ElementType: types.StringType,
			Required:    true,
		},
	}
	if r.scope.ownerAttribute != "" {
		attributes[r.scope.ownerAttribute] = schema.StringAttribute{
			MarkdownDescription: r.scope.ownerDescription,
			Required:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: r.scope.description,
		Attributes:          attributes,
	}
}

func (r *featureFlagOverridesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *featureFlagOverridesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	owner, desired, diags := r.readModel(ctx, req.Plan.GetAttribute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	applied, err := r.applyFlags(ctx, owner, map[string]string{}, desired)
	if err != nil {
		if !r.scope.removable {
			if len(applied) > 0 {
				resp.Diagnostics.AddWarning(
					fmt.Sprintf("%s Feature Flag Overrides Not Removed", r.scope.label),
					fmt.Sprintf("Kinde has no API to remove %s feature flag overrides, so these overrides applied before the create failed remain: %s.",
						strings.ToLower(r.scope.label), strings.Join(sortedKeys(applied), ", ")),
				)
			}
		} else if _, cleanupErr := r.applyFlags(ctx, owner, applied, map[string]string{}); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				fmt.Sprintf("Error Cleaning Up %s Feature Flag Overrides", r.scope.label),
				fmt.Sprintf("Could not remove overrides applied before the create failed. Remove them in Kinde: %s", cleanupErr),
			)
		}
		resp.Diagnostics.AddError(fmt.Sprintf("Error Creating %s Feature Flag Overrides", r.scope.label), err.Error())
		return
	}

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, owner, applied)...)
}

func (r *featureFlagOverridesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	owner, managed, diags := r.readModel(ctx, req.State.GetAttribute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	current, err := listFeatureFlagValues(ctx, r.client, r.scope.listPath(owner))
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error Reading %s Feature Flags", r.scope.label), err.Error())
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

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, owner, refreshed)...)
}

func (r *featureFlagOverridesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	owner, desired, diags := r.readModel(ctx, req.Plan.GetAttribute)
	resp.Diagnostics.Append(diags...)
	_, previous, diags := r.readModel(ctx, req.State.GetAttribute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	applied, err := r.applyFlags(ctx, owner, previous, desired)
	resp.Diagnostics.Append(r.setState(ctx, &resp.State, owner, applied)...)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error Updating %s Feature Flag Overrides", r.scope.label), err.Error())
		return
	}

	if !r.scope.removable {
		var kept []string
		for _, key := range sortedKeys(previous) {
			if _, ok := desired[key]; !ok {
				kept = append(kept, key)
			}
		}
		if len(kept) > 0 {
			resp.Diagnostics.AddWarning(
				fmt.Sprintf("%s Feature Flag Overrides Not Removed", r.scope.label),
				fmt.Sprintf("Kinde has no API to remove %s feature flag overrides. These overrides are no longer managed by Terraform but keep their current value in Kinde: %s.",
					strings.ToLower(r.scope.label), strings.Join(kept, ", ")),
			)
		}
	}
}

func (r *featureFlagOverridesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	owner, managed, diags := r.readModel(ctx, req.State.GetAttribute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.scope.removable {
		if len(managed) > 0 {
			resp.Diagnostics.AddWarning(
				fmt.Sprintf("%s Feature Flag Overrides Not Removed", r.scope.label),
				fmt.Sprintf("Kinde has no API to remove %s feature flag overrides. These overrides are no longer managed by Terraform but keep their current value in Kinde: %s.",
					strings.ToLower(r.scope.label), strings.Join(sortedKeys(managed), ", ")),
			)
		}
		return
	}

	if _, err := r.applyFlags(ctx, owner, managed, map[string]string{}); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error Deleting %s Feature Flag Overrides", r.scope.label), err.Error())
	}
}

func (r *featureFlagOverridesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	owner, keys := "", req.ID
	expected := "a comma-separated list of feature flag keys, e.g. \"dark_mode,max_seats\""
	if r.scope.ownerAttribute != "" {
		expected = fmt.Sprintf("%s:key1,key2, e.g. \"abc123:dark_mode,max_seats\"", r.scope.ownerAttribute)
		var found bool
		owner, keys, found = strings.Cut(req.ID, ":")
		owner = strings.TrimSpace(owner)
		if !found || owner == "" {
			resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Expected %s.", expected))
			return
		}
	}

	flags := map[string]string{}
	for _, key := range strings.Split(keys, ",") {
		if key = strings.TrimSpace(key); key != "" {
			flags[key] = ""
		}
	}

	if len(flags) == 0 {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Expected %s.", expected))
		return
	}

	resp.Diagnostics.Append(r.setState(ctx, &resp.State, owner, flags)...)
}

func (r *featureFlagOverridesResource) readModel(ctx context.Context, get func(context.Context, path.Path, any) diag.Diagnostics) (string, map[string]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	var owner types.String
	if r.scope.ownerAttribute != "" {
		diags.Append(get(ctx, path.Root(r.scope.ownerAttribute), &owner)...)
	}

	var flagsValue types.Map
	diags.Append(get(ctx, path.Root("flags"), &flagsValue)...)
	if diags.HasError() {
		return "", nil, diags
	}

	flags, flagDiags := flagsFromModel(ctx, flagsValue)
	diags.Append(flagDiags...)
	return owner.ValueString(), flags, diags
}

func (r *featureFlagOverridesResource) setState(ctx context.Context, state *tfsdk.State, owner string, flags map[string]string) diag.Diagnostics {
	flagsValue, diags := types.MapValueFrom(ctx, types.StringType, flags)
	if diags.HasError() {
		return diags
	}

	id := r.scope.fixedID
	if r.scope.ownerAttribute != "" {
		id = owner
		diags.Append(state.SetAttribute(ctx, path.Root(r.scope.ownerAttribute), types.StringValue(owner))...)
	}
	diags.Append(state.SetAttribute(ctx, path.Root("id"), types.StringValue(id))...)
	diags.Append(state.SetAttribute(ctx, path.Root("flags"), flagsValue)...)
	return diags
}

func (r *featureFlagOverridesResource) applyFlags(ctx context.Context, owner string, previous, desired map[string]string) (map[string]string, error) {
	applied := make(map[string]string, len(previous))
	for key, value := range previous {
		applied[key] = value
	}

	for _, key := range sortedKeys(previous) {
		if _, keep := desired[key]; keep {
			continue
		}
		if r.scope.removable {
			if err := r.scope.deleteOverride(ctx, r.client, owner, key); err != nil {
				return applied, err
			}
		}
		delete(applied, key)
	}

	for _, key := range sortedKeys(desired) {
		value := desired[key]
		if current, ok := previous[key]; ok && current == value {
			continue
		}
		if err := r.scope.setOverride(ctx, r.client, owner, key, value); err != nil {
			return applied, err
		}
		applied[key] = value
	}

	return applied, nil
}

func listFeatureFlagValues(ctx context.Context, client *ManagementAPI, endpoint string) (map[string]string, error) {
	var body struct {
		FeatureFlags map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"feature_flags"`
	}
	if err := client.getJSON(ctx, endpoint, &body); err != nil {
		return nil, fmt.Errorf("could not list feature flags: %w", err)
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
