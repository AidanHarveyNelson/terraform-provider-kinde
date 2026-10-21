// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client *ManagementAPI
}

func identityKey(identityType, value string) string {
	return identityType + ":" + value
}

type UserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	FirstName        types.String `tfsdk:"first_name"`
	LastName         types.String `tfsdk:"last_name"`
	IsSuspended      types.Bool   `tfsdk:"is_suspended"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	CreatedOn        types.String `tfsdk:"created_on"`
	UpdatedOn        types.String `tfsdk:"updated_on"`
	Identities       types.Set    `tfsdk:"identities"`
}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user within a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier for the user.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"first_name": schema.StringAttribute{
				Description:         "The first name of the user.",
				Optional:            true,
				MarkdownDescription: "The first name of the user.",
			},
			"last_name": schema.StringAttribute{
				Description:         "The last name of the user.",
				Optional:            true,
				MarkdownDescription: "The last name of the user.",
			},
			"is_suspended": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the user is suspended.",
			},
			"organization_code": schema.StringAttribute{
				Optional:            true,
				Description:         "The code of the organization the user belongs to.",
				MarkdownDescription: "Organization code to create the user in. This value is sent to the API only on create and the API does not return it in read responses.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"created_on": schema.StringAttribute{
				Description: "The timestamp when the user was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_on": schema.StringAttribute{
				Description: "The timestamp when the user was last updated.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"identities": schema.SetNestedAttribute{
				Description: "Identities for the user (email, username, phone, etc.).",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Description: "The type of identity (email, username, phone, enterprise, social).",
							Required:    true,
						},
						"value": schema.StringAttribute{
							Description: "The value of the identity.",
							Required:    true,
						},
					},
				},
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Debug(ctx, "Starting user creation")

	var plan UserResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if is_suspended is set to true on create, which is not allowed
	if !plan.IsSuspended.IsNull() && plan.IsSuspended.ValueBool() {
		resp.Diagnostics.AddError(
			"Invalid Configuration",
			"Setting is_suspended=true when creating a user is not supported. Create the user first, then update the is_suspended attribute.",
		)
		return
	}

	if plan.FirstName.IsNull() || plan.FirstName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing First Name",
			"first_name must be set when creating a user.",
		)
		return
	}

	if plan.LastName.IsNull() || plan.LastName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing Last Name",
			"last_name must be set when creating a user.",
		)
		return
	}

	// Validate that at least one email identity is provided
	var identities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}
	diags = plan.Identities.ElementsAs(ctx, &identities, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasEmail := false
	var createIdentities []kindeIdentity
	for _, identity := range identities {
		if identity.Type == identityTypeEmail {
			hasEmail = true
		}
		details := make(map[string]string)
		switch identity.Type {
		case identityTypeEmail:
			details["email"] = identity.Value
		case identityTypeUsername:
			details["username"] = identity.Value
		case identityTypePhone:
			details["phone"] = identity.Value
		}
		createIdentities = append(createIdentities, kindeIdentity{
			Type:    identity.Type,
			Details: details,
		})
	}

	if !hasEmail {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	profile := userProfile{
		GivenName:  plan.FirstName.ValueString(),
		FamilyName: plan.LastName.ValueString(),
	}

	// Create user with profile and identities
	createParams := userCreateParams{
		Profile:    profile,
		OrgCode:    plan.OrganizationCode.ValueString(),
		Identities: createIdentities,
	}

	userID, err := r.createUser(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating User",
			fmt.Sprintf("Could not create user: %s", err),
		)
		return
	}

	// Get the final state of the user
	user, err := r.getUser(ctx, userID)
	if err != nil {
		if cleanupErr := r.cleanupUserOnCreateFailure(ctx, userID); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"User Create Rollback Failed",
				fmt.Sprintf("Could not read created user and automatic rollback also failed for user ID %s: %s. Manual intervention may be required to delete the partially created user.", userID, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading Created User",
			fmt.Sprintf("Could not read created user ID %s: %s", userID, err),
		)
		return
	}

	// Get final identities
	finalIdentities, err := r.getUserIdentities(ctx, userID)
	if err != nil {
		if cleanupErr := r.cleanupUserOnCreateFailure(ctx, userID); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"User Create Rollback Failed",
				fmt.Sprintf("Could not read identities for created user and automatic rollback also failed for user ID %s: %s. Manual intervention may be required to delete the partially created user.", userID, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", userID, err),
		)
		return
	}

	// Convert final identities to Terraform state format
	var tfIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}

	// Create a map of planned identity values to types for reference
	plannedIdentityTypes := make(map[string]string)
	for _, identity := range identities {
		plannedIdentityTypes[identity.Value] = identity.Type
	}

	for _, identity := range finalIdentities {
		// Skip OAuth2 identities when storing in state
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}

		// Use the type from plan if available, otherwise use API type
		identityType := identity.Type
		if plannedType, exists := plannedIdentityTypes[identity.Name]; exists {
			identityType = plannedType
		}

		tfIdentities = append(tfIdentities, struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}{
			Type:  identityType,
			Value: identity.Name,
		})
	}

	// Sort identities consistently by type and then by value
	sort.Slice(tfIdentities, func(i, j int) bool {
		if tfIdentities[i].Type == tfIdentities[j].Type {
			return tfIdentities[i].Value < tfIdentities[j].Value
		}
		return tfIdentities[i].Type < tfIdentities[j].Type
	})

	// Convert identities to set
	identitiesSet, diags := types.SetValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":  types.StringType,
			"value": types.StringType,
		},
	}, tfIdentities)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Set all fields from API response
	plan.ID = types.StringValue(user.ID)

	// Handle first_name: only set if it was in the plan
	if !plan.FirstName.IsNull() {
		plan.FirstName = types.StringValue(user.FirstName)
	} else {
		plan.FirstName = types.StringNull()
	}

	// Handle last_name: only set if it was in the plan
	if !plan.LastName.IsNull() {
		plan.LastName = types.StringValue(user.LastName)
	} else {
		plan.LastName = types.StringNull()
	}

	plan.CreatedOn = types.StringValue(user.CreatedOn.String())
	plan.UpdatedOn = types.StringValue(user.UpdatedOn.String())
	plan.Identities = identitiesSet

	// Only set is_suspended in state if it was explicitly configured in the plan
	if !plan.IsSuspended.IsNull() {
		plan.IsSuspended = types.BoolValue(user.IsSuspended)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// cleanupUserOnCreateFailure best-effort deletes a user that was successfully
// created but could not be fully read back, so that a failed Create does not
// leave an orphaned user that Terraform state never tracks (and that
// `terraform destroy` therefore cannot clean up).
func (r *UserResource) cleanupUserOnCreateFailure(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}

	if err := r.deleteUser(ctx, userID); err != nil && !isNotFoundError(err) {
		tflog.Warn(ctx, "Failed to clean up user after create failure", map[string]interface{}{
			"id":    userID,
			"error": err.Error(),
		})
		return err
	}

	return nil
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.getUser(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	// Get user identities
	identities, err := r.getUserIdentities(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	// Convert identities to Terraform state
	var tfIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}

	// If we have existing state identities, use them to preserve the types
	var stateIdentitiesMap map[string]string
	if !state.Identities.IsNull() {
		stateIdentitiesMap = make(map[string]string)
		var stateIdentities []struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}
		diags = state.Identities.ElementsAs(ctx, &stateIdentities, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Create a map of value -> type from state
		for _, identity := range stateIdentities {
			stateIdentitiesMap[identity.Value] = identity.Type
		}
	}

	// Process API identities
	for _, identity := range identities {
		// Skip OAuth2 identities when storing in state
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}

		// Use the type from state if available, otherwise use API type
		identityType := identity.Type
		if stateIdentitiesMap != nil {
			if stateType, exists := stateIdentitiesMap[identity.Name]; exists {
				identityType = stateType
			}
		}

		tfIdentities = append(tfIdentities, struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}{
			Type:  identityType,
			Value: identity.Name,
		})
	}

	// Sort identities consistently by type and then by value
	sort.Slice(tfIdentities, func(i, j int) bool {
		if tfIdentities[i].Type == tfIdentities[j].Type {
			return tfIdentities[i].Value < tfIdentities[j].Value
		}
		return tfIdentities[i].Type < tfIdentities[j].Type
	})

	// Update state with user data
	state.ID = types.StringValue(user.ID)

	// Handle first_name: only set if it was previously set in state
	if !state.FirstName.IsNull() {
		state.FirstName = types.StringValue(user.FirstName)
	}

	// Handle last_name: only set if it was previously set in state
	if !state.LastName.IsNull() {
		state.LastName = types.StringValue(user.LastName)
	}

	// Only set is_suspended in state if it was previously configured
	if !state.IsSuspended.IsNull() {
		state.IsSuspended = types.BoolValue(user.IsSuspended)
	}

	state.CreatedOn = types.StringValue(user.CreatedOn.String())
	state.UpdatedOn = types.StringValue(user.UpdatedOn.String())

	// Convert identities to set
	identitiesSet, diags := types.SetValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":  types.StringType,
			"value": types.StringType,
		},
	}, tfIdentities)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Identities = identitiesSet

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Debug(ctx, "Starting user update")

	var plan, state UserResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if first_name was previously set and is now being omitted or set to empty
	if !state.FirstName.IsNull() && (plan.FirstName.IsNull() || plan.FirstName.ValueString() == "") {
		resp.Diagnostics.AddError(
			"Cannot Reset First Name",
			"The Kinde API does not allow resetting first_name once it has been set. Please provide the existing first_name value in your configuration.",
		)
	}

	// Check if last_name was previously set and is now being omitted or set to empty
	if !state.LastName.IsNull() && (plan.LastName.IsNull() || plan.LastName.ValueString() == "") {
		resp.Diagnostics.AddError(
			"Cannot Reset Last Name",
			"The Kinde API does not allow resetting last_name once it has been set. Please provide the existing last_name value in your configuration.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Get the user
	_, err := r.getUser(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Always initialize empty update params
	updateParams := userUpdateParams{}

	// Only set FirstName if it's not null and not empty
	if !plan.FirstName.IsNull() && plan.FirstName.ValueString() != "" {
		firstName := plan.FirstName.ValueString()
		updateParams.GivenName = firstName
	} else {
		// Preserve existing first_name from state
		updateParams.GivenName = state.FirstName.ValueString()
	}

	// Only set LastName if it's not null and not empty
	if !plan.LastName.IsNull() && plan.LastName.ValueString() != "" {
		lastName := plan.LastName.ValueString()
		updateParams.FamilyName = lastName
	} else {
		// Preserve existing last_name from state
		updateParams.FamilyName = state.LastName.ValueString()
	}

	// Only include is_suspended in update if it's explicitly configured
	if !plan.IsSuspended.IsNull() {
		isSuspended := plan.IsSuspended.ValueBool()
		updateParams.IsSuspended = &isSuspended
	}

	err = r.updateUser(ctx, plan.ID.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating User",
			fmt.Sprintf("Could not update user ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Get current identities from the API to identify OAuth2 identities
	currentIdentities, err := r.getUserIdentities(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	managedIdentities := make(map[string]kindeIdentity)

	// Extract OAuth2 identities to preserve (we won't add them to state, but we need to avoid removing them)
	var oauth2Identities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}
	for _, identity := range currentIdentities {
		if strings.HasPrefix(identity.Type, "oauth2:") {
			oauth2Identities = append(oauth2Identities, struct {
				Type  string `tfsdk:"type"`
				Value string `tfsdk:"value"`
			}{
				Type:  identity.Type,
				Value: identity.Name,
			})
			continue
		}

		key := identityKey(identity.Type, identity.Name)
		managedIdentities[key] = identity
	}

	// Get planned identities
	var plannedIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}
	diags = plan.Identities.ElementsAs(ctx, &plannedIdentities, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// For validation and identity management, we need to consider OAuth identities
	// but we won't include them in the final state
	allIdentities := append(plannedIdentities, oauth2Identities...)

	// Validate that at least one email identity is provided
	hasEmail := false
	for _, identity := range allIdentities {
		if identity.Type == identityTypeEmail {
			hasEmail = true
			break
		}
	}

	if !hasEmail {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	existingIdentityKeys := make(map[string]struct{})
	for key := range managedIdentities {
		existingIdentityKeys[key] = struct{}{}
	}
	for _, identity := range oauth2Identities {
		existingIdentityKeys[identityKey(identity.Type, identity.Value)] = struct{}{}
	}

	desiredIdentityKeys := make(map[string]struct{})
	for _, identity := range plannedIdentities {
		// Skip OAuth2 identities as they are managed externally
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}

		key := identityKey(identity.Type, identity.Value)
		desiredIdentityKeys[key] = struct{}{}

		if _, exists := existingIdentityKeys[key]; exists {
			continue
		}

		addIdentityParams := userIdentityAddParams{
			Type:  identity.Type,
			Value: identity.Value,
		}

		err := r.addUserIdentity(ctx, plan.ID.ValueString(), addIdentityParams)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Adding User Identity",
				fmt.Sprintf("Could not add identity to user %s: %s", plan.ID.ValueString(), err),
			)
			return
		}

		existingIdentityKeys[key] = struct{}{}
	}

	for key, identity := range managedIdentities {
		if _, keep := desiredIdentityKeys[key]; keep {
			continue
		}

		if err := r.deleteIdentity(ctx, identity.ID); err != nil {
			resp.Diagnostics.AddError(
				"Error Removing User Identity",
				fmt.Sprintf("Could not remove identity %s from user %s: %s", identity.Name, plan.ID.ValueString(), err),
			)
			return
		}
	}

	// Get final state of the user
	user, err := r.getUser(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated User",
			fmt.Sprintf("Could not read updated user %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Get final identities
	finalIdentities, err := r.getUserIdentities(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Convert final identities to Terraform state format, excluding OAuth2 identities
	var tfIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}

	// Create a map of planned identity values to types for reference
	plannedIdentityTypes := make(map[string]string)
	for _, identity := range plannedIdentities {
		plannedIdentityTypes[identity.Value] = identity.Type
	}

	for _, identity := range finalIdentities {
		// Skip OAuth2 identities when storing in state
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}

		// Use the type from plan if available, otherwise use API type
		identityType := identity.Type
		if plannedType, exists := plannedIdentityTypes[identity.Name]; exists {
			identityType = plannedType
		}

		tfIdentities = append(tfIdentities, struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}{
			Type:  identityType,
			Value: identity.Name,
		})
	}

	// Sort identities consistently by type and then by value
	sort.Slice(tfIdentities, func(i, j int) bool {
		if tfIdentities[i].Type == tfIdentities[j].Type {
			return tfIdentities[i].Value < tfIdentities[j].Value
		}
		return tfIdentities[i].Type < tfIdentities[j].Type
	})

	// Convert identities to set
	identitiesSet, diags := types.SetValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":  types.StringType,
			"value": types.StringType,
		},
	}, tfIdentities)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update plan with final state
	plan.Identities = identitiesSet

	// Only set name fields in state if they were in the plan
	// This ensures that omitted fields stay omitted
	if !plan.FirstName.IsNull() {
		plan.FirstName = types.StringValue(user.FirstName)
	}
	if !plan.LastName.IsNull() {
		plan.LastName = types.StringValue(user.LastName)
	}

	// Only set is_suspended in state if it was explicitly configured
	if !plan.IsSuspended.IsNull() {
		plan.IsSuspended = types.BoolValue(user.IsSuspended)
	}

	plan.CreatedOn = types.StringValue(user.CreatedOn.String())
	plan.UpdatedOn = types.StringValue(user.UpdatedOn.String())

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.deleteUser(ctx, state.ID.ValueString()); err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting User",
			fmt.Sprintf("Could not delete user ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Get the user by ID
	user, err := r.getUser(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", req.ID, err),
		)
		return
	}

	// Get user identities
	identities, err := r.getUserIdentities(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", req.ID, err),
		)
		return
	}

	// Convert identities to Terraform state format
	var tfIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}
	for _, identity := range identities {
		// Skip OAuth2 identities to match Read/Update behavior.
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}
		tfIdentities = append(tfIdentities, struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}{
			Type:  identity.Type,
			Value: identity.Name,
		})
	}

	// Convert identities to set
	identitiesSet, diags := types.SetValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":  types.StringType,
			"value": types.StringType,
		},
	}, tfIdentities)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Set all fields in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), user.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("first_name"), user.FirstName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("last_name"), user.LastName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("created_on"), user.CreatedOn.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("updated_on"), user.UpdatedOn.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("identities"), identitiesSet)...)
}

const (
	identityTypeEmail    = "email"
	identityTypeUsername = "username"
	identityTypePhone    = "phone"
)

type kindeUser struct {
	ID             string     `json:"id"`
	ProvidedID     string     `json:"provided_id,omitempty"`
	PreferredEmail string     `json:"preferred_email"`
	LastName       string     `json:"last_name"`
	FirstName      string     `json:"first_name"`
	IsSuspended    bool       `json:"is_suspended"`
	Picture        string     `json:"picture,omitempty"`
	CreatedOn      time.Time  `json:"created_on"`
	LastSignedIn   *time.Time `json:"last_signed_in,omitempty"`
	UpdatedOn      time.Time  `json:"updated_on"`
}

type kindeIdentity struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	CreatedOn   time.Time         `json:"created_on"`
	IsConfirmed *bool             `json:"is_confirmed"`
	TotalLogins int               `json:"total_logins"`
	LastLoginOn *time.Time        `json:"last_login_on"`
	Details     map[string]string `json:"details"`
}

type userProfile struct {
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Email      string `json:"email"`
	ProvidedID string `json:"provided_id,omitempty"`
}

type userCreateParams struct {
	Profile    userProfile     `json:"profile"`
	Identities []kindeIdentity `json:"identities,omitempty"`
	Password   string          `json:"password,omitempty"`
	OrgCode    string          `json:"org_code,omitempty"`
}

type userUpdateParams struct {
	GivenName   string `json:"given_name,omitempty"`
	FamilyName  string `json:"family_name,omitempty"`
	ProvidedID  string `json:"provided_id,omitempty"`
	IsSuspended *bool  `json:"is_suspended,omitempty"`
}

type userIdentityAddParams struct {
	Value          string `json:"value"`
	Type           string `json:"type"`
	PhoneCountryID string `json:"phone_country_id,omitempty"`
	ConnectionID   string `json:"connection_id,omitempty"`
}

func userPath(id string) string {
	return "/api/v1/user?id=" + url.QueryEscape(id)
}

func userIdentitiesPath(id string) string {
	return "/api/v1/users/" + url.PathEscape(id) + "/identities"
}

func (r *UserResource) createUser(ctx context.Context, params userCreateParams) (string, error) {
	var response struct {
		ID string `json:"id"`
	}
	if err := r.client.doJSON(ctx, http.MethodPost, "/api/v1/user", params, &response); err != nil {
		return "", err
	}
	return response.ID, nil
}

func (r *UserResource) getUser(ctx context.Context, id string) (*kindeUser, error) {
	var user kindeUser
	if err := r.client.getJSON(ctx, userPath(id), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserResource) updateUser(ctx context.Context, id string, params userUpdateParams) error {
	return r.client.doJSON(ctx, http.MethodPatch, userPath(id), params, nil)
}

func (r *UserResource) deleteUser(ctx context.Context, id string) error {
	res, err := r.client.DeleteUser(ctx, management_api.DeleteUserParams{ID: id})
	_, err = expectResponse[management_api.SuccessResponse]("delete user", res, err)
	return err
}

func (r *UserResource) getUserIdentities(ctx context.Context, id string) ([]kindeIdentity, error) {
	var response struct {
		Identities []kindeIdentity `json:"identities"`
	}
	if err := r.client.getJSON(ctx, userIdentitiesPath(id), &response); err != nil {
		return nil, err
	}
	return response.Identities, nil
}

func (r *UserResource) addUserIdentity(ctx context.Context, id string, params userIdentityAddParams) error {
	if params.Type == identityTypePhone {
		formattedNumber, err := formatPhoneNumber(params.Value, params.PhoneCountryID)
		if err != nil {
			return fmt.Errorf("invalid phone number components: %w", err)
		}

		localNumber, countryID, err := parsePhoneNumber(formattedNumber)
		if err != nil {
			return fmt.Errorf("invalid phone number: %w", err)
		}

		params.Value = localNumber
		params.PhoneCountryID = countryID
	}

	return r.client.doJSON(ctx, http.MethodPost, userIdentitiesPath(id), params, nil)
}

func (r *UserResource) deleteIdentity(ctx context.Context, identityID string) error {
	res, err := r.client.DeleteIdentity(ctx, management_api.DeleteIdentityParams{IdentityID: identityID})
	_, err = expectResponse[management_api.SuccessResponse]("delete identity", res, err)
	return err
}
