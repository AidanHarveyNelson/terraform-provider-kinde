// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/organizations"
)

var (
	_ resource.Resource                = &OrganizationUserResource{}
	_ resource.ResourceWithImportState = &OrganizationUserResource{}
)

func NewOrganizationUserResource() resource.Resource {
	return &OrganizationUserResource{}
}

type OrganizationUserResource struct {
	client *organizations.Client
}

func (r *OrganizationUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_user"
}

func (r *OrganizationUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a user's membership and roles in a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The composite ID of the organization user membership.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_code": schema.StringAttribute{
				Required:    true,
				Description: "The code of the organization.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"roles": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "The list of role IDs to assign to the user.",
			},
		},
	}
}

func (r *OrganizationUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	r.client = client.Organizations
}

func (r *OrganizationUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationUserResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Parse desired roles up front (if provided)
	var roles []string
	if !plan.Roles.IsNull() {
		diags = plan.Roles.ElementsAs(ctx, &roles, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	orgCode := plan.OrganizationCode.ValueString()
	userID := plan.UserID.ValueString()

	// Check if the user is already a member so Create is idempotent.
	// GetUserRoles returns an error when the membership is missing.
	_, err := r.client.GetUserRoles(ctx, orgCode, userID)
	memberExists := err == nil
	if err != nil && !isNotFoundError(err) && !isUserNotInOrganizationError(err) {
		resp.Diagnostics.AddError(
			"Error Reading Organization User",
			fmt.Sprintf("Could not check organization user membership: %s", err),
		)
		return
	}

	addedMembershipThisRun := false
	if !memberExists {
		// First, add user to organization without roles
		params := organizations.AddUsersParams{
			Users: []organizations.AddUser{{ID: userID}},
		}

		err := r.client.AddUsers(ctx, orgCode, params)
		if err != nil {
			// If the user was added concurrently, treat conflict as success.
			if !hasStatusCode(err, http.StatusConflict) {
				resp.Diagnostics.AddError(
					"Error Creating Organization User",
					fmt.Sprintf("Could not create organization user: %s", err),
				)
				return
			}
		} else {
			addedMembershipThisRun = true
		}
	}

	// Then, if roles are specified, add them one by one
	for _, roleID := range roles {
		err := r.client.AddUserRole(ctx, orgCode, userID, roleID)
		if err != nil {
			// Best-effort rollback if membership was created in this call.
			if addedMembershipThisRun {
				if rollbackErr := r.removeUserFromOrganization(ctx, orgCode, userID); rollbackErr != nil {
					resp.Diagnostics.AddWarning(
						"Partial Create Rollback Failed",
						fmt.Sprintf("Role assignment failed and rollback also failed: %s", rollbackErr),
					)
				}
			}
			resp.Diagnostics.AddError(
				"Error Adding Role",
				fmt.Sprintf("Could not add role %s: %s", roleID, err),
			)
			return
		}
	}

	// Set ID
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.OrganizationCode.ValueString(), plan.UserID.ValueString()))

	// Set state
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationUserResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get user roles to verify membership
	roles, err := r.client.GetUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if err != nil {
		if isNotFoundError(err) || isUserNotInOrganizationError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Organization User",
			fmt.Sprintf("Could not read organization user: %s", err),
		)
		return
	}

	// Convert roles to IDs
	roleIDs := make([]string, len(roles))
	for i, role := range roles {
		roleIDs[i] = role.ID
	}

	// If there are roles, set them in state, otherwise set to null
	if len(roleIDs) > 0 {
		rolesList, diags := types.ListValueFrom(ctx, types.StringType, roleIDs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Roles = rolesList
	} else {
		state.Roles = types.ListNull(types.StringType)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Handle role updates
	if !plan.Roles.Equal(state.Roles) {
		// Get current roles from API to ensure we have the latest state
		currentRoles, err := r.client.GetUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading Current Roles",
				fmt.Sprintf("Could not read current roles: %s", err),
			)
			return
		}

		// Convert current roles to a slice of IDs
		currentRoleIDs := make([]string, len(currentRoles))
		for i, role := range currentRoles {
			currentRoleIDs[i] = role.ID
		}

		// Get desired roles from plan
		var desiredRoles []string
		if !plan.Roles.IsNull() {
			diags := plan.Roles.ElementsAs(ctx, &desiredRoles, false)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		// Remove roles that are not in the desired set
		for _, roleID := range currentRoleIDs {
			found := false
			for _, desiredRole := range desiredRoles {
				if roleID == desiredRole {
					found = true
					break
				}
			}
			if !found {
				err := r.client.RemoveUserRole(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString(), roleID)
				if err != nil {
					resp.Diagnostics.AddError(
						"Error Removing Role",
						fmt.Sprintf("Could not remove role %s: %s", roleID, err),
					)
					return
				}
			}
		}

		// Add roles that are not in the current set
		for _, roleID := range desiredRoles {
			found := false
			for _, currentRole := range currentRoleIDs {
				if roleID == currentRole {
					found = true
					break
				}
			}
			if !found {
				err := r.client.AddUserRole(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString(), roleID)
				if err != nil {
					resp.Diagnostics.AddError(
						"Error Adding Role",
						fmt.Sprintf("Could not add role %s: %s", roleID, err),
					)
					return
				}
			}
		}
	}

	// Set state
	diags := resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationUserResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.removeUserFromOrganization(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString()); err != nil {
		if isNotFoundError(err) || isUserNotInOrganizationError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Removing User from Organization",
			fmt.Sprintf("Could not remove user from organization: %s", err),
		)
		return
	}
}

func (r *OrganizationUserResource) removeUserFromOrganization(ctx context.Context, organizationCode, userID string) error {
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/users/%s", organizationCode, userID)
	request, err := r.client.NewRequest(ctx, "DELETE", endpoint, nil, nil)
	if err != nil {
		return err
	}

	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	return r.client.DoRequest(request, &response)
}

func (r *OrganizationUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: organization_code:user_id
	idParts, err := splitID(req.ID, 2, "organization_code:user_id")
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_code"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
