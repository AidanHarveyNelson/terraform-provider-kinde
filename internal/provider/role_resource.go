// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	_ resource.Resource                = &RoleResource{}
	_ resource.ResourceWithImportState = &RoleResource{}
)

func NewRoleResource() resource.Resource {
	return &RoleResource{}
}

type RoleResource struct {
	client *ManagementAPI
}

type nonEmptyStringSetValidator struct{}

func (v nonEmptyStringSetValidator) Description(_ context.Context) string {
	return "each permission ID must be non-empty"
}

func (v nonEmptyStringSetValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v nonEmptyStringSetValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	for index, element := range req.ConfigValue.Elements() {
		stringValue, ok := element.(basetypes.StringValue)
		if !ok {
			continue
		}

		if stringValue.IsNull() || stringValue.IsUnknown() {
			continue
		}

		if strings.TrimSpace(stringValue.ValueString()) == "" {
			resp.Diagnostics.Append(diag.NewAttributeErrorDiagnostic(
				req.Path.AtSetValue(stringValue),
				"Invalid Permission ID",
				fmt.Sprintf("Permission ID at index %d must not be empty.", index),
			))
		}
	}
}

func (r *RoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *RoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Roles represent collections of permissions that can be assigned to users. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/roles) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the role",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the role",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the role. This field is required because the Kinde API does not properly handle unsetting or empty descriptions once they are set. To maintain consistent behavior and prevent state drift, we require a description for all roles.",
				Required:            true,
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "List of permission IDs associated with this role",
				Optional:            true,
				ElementType:         types.StringType,
				Validators:          []validator.Set{nonEmptyStringSetValidator{}},
			},
		},
	}
}

func (r *RoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func diffPermissions(desired, current []string) ([]string, []string) {
	desired = normalizeRolePermissions(desired)
	current = normalizeRolePermissions(current)

	desiredSet := make(map[string]struct{}, len(desired))
	for _, permission := range desired {
		desiredSet[permission] = struct{}{}
	}

	currentSet := make(map[string]struct{}, len(current))
	for _, permission := range current {
		currentSet[permission] = struct{}{}
	}

	toRemove := make([]string, 0)
	for _, permission := range current {
		if _, found := desiredSet[permission]; !found {
			toRemove = append(toRemove, permission)
		}
	}

	toAdd := make([]string, 0)
	for _, permission := range desired {
		if _, found := currentSet[permission]; !found {
			toAdd = append(toAdd, permission)
		}
	}

	return toRemove, toAdd
}

func (r *RoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create role with basic details first
	createParams := expandRoleCreateParams(plan)
	role, err := r.createRole(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			fmt.Sprintf("Could not create role: %s", err),
		)
		return
	}
	createdRoleID := role.ID

	// Get the complete role data
	role, err = r.getRole(ctx, createdRoleID)
	if err != nil {
		if cleanupErr := r.cleanupRoleOnCreateFailure(ctx, createdRoleID); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"Role Create Rollback Failed",
				fmt.Sprintf("Could not read created role and automatic rollback also failed for role ID %s: %s. Manual intervention may be required to delete the partially created role.", createdRoleID, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading Created Role",
			fmt.Sprintf("Could not read created role: %s", err),
		)
		return
	}

	// Update permissions if specified
	if !plan.Permissions.IsNull() {
		var permissions []string
		diags = plan.Permissions.ElementsAs(ctx, &permissions, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Normalize permissions for consistent ordering and to drop invalid empty IDs.
		sorted := normalizeRolePermissions(permissions)

		permissionItems := make([]kindeRolePermissionUpdate, len(sorted))
		for i, p := range sorted {
			permissionItems[i] = kindeRolePermissionUpdate{
				ID: p,
			}
		}

		err = r.updateRolePermissions(ctx, createdRoleID, permissionItems)
		if err != nil {
			if cleanupErr := r.cleanupRoleOnCreateFailure(ctx, createdRoleID); cleanupErr != nil {
				resp.Diagnostics.AddWarning(
					"Role Create Rollback Failed",
					fmt.Sprintf("Could not set role permissions and automatic rollback also failed for role ID %s: %s. Manual intervention may be required to delete the partially created role.", createdRoleID, cleanupErr),
				)
			}
			resp.Diagnostics.AddError(
				"Error Setting Role Permissions",
				fmt.Sprintf("Could not set permissions for role: %s", err),
			)
			return
		}

		// Get the updated role to ensure we have all fields and permissions
		role, err = r.getRole(ctx, createdRoleID)
		if err != nil {
			if cleanupErr := r.cleanupRoleOnCreateFailure(ctx, createdRoleID); cleanupErr != nil {
				resp.Diagnostics.AddWarning(
					"Role Create Rollback Failed",
					fmt.Sprintf("Could not read updated role and automatic rollback also failed for role ID %s: %s. Manual intervention may be required to delete the partially created role.", createdRoleID, cleanupErr),
				)
			}
			resp.Diagnostics.AddError(
				"Error Reading Updated Role",
				fmt.Sprintf("Could not read updated role: %s", err),
			)
			return
		}
	}

	state, err := flattenRoleResource(ctx, role, role.Permissions)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) cleanupRoleOnCreateFailure(ctx context.Context, roleID string) error {
	if roleID == "" {
		return nil
	}

	if err := r.deleteRole(ctx, roleID); err != nil && !isNotFoundError(err) {
		return err
	}

	return nil
}

func (r *RoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, err := r.getRole(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Role",
			fmt.Sprintf("Could not read role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	updatedState, err := flattenRoleResource(ctx, role, role.Permissions)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}
	state = updatedState

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state for comparison
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// First update role details
	updateParams := expandRoleUpdateParams(plan)
	res, err := r.client.UpdateRoles(ctx,
		management_api.NewOptUpdateRolesReq(updateParams),
		management_api.UpdateRolesParams{RoleID: plan.ID.ValueString()},
	)
	if _, err = expectResponse[management_api.SuccessResponse]("update role", res, err); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Role",
			fmt.Sprintf("Could not update role ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Handle permissions update if the field is set in the plan
	var planPerms []string
	if !plan.Permissions.IsNull() {
		diags = plan.Permissions.ElementsAs(ctx, &planPerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Get current permissions from state
	var statePerms []string
	if !state.Permissions.IsNull() {
		diags = state.Permissions.ElementsAs(ctx, &statePerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	permsToRemove, permsToAdd := diffPermissions(planPerms, statePerms)

	// First remove permissions that are in state but not in plan.
	for _, statePerm := range permsToRemove {
		err = r.removeRolePermission(ctx, plan.ID.ValueString(), statePerm)
		if err != nil {
			if containsAnyErrorCode(err, "INVALID_PERMISSION_ID", "INVALID_PERMISSION_ID_ROLE") {
				continue
			}
			resp.Diagnostics.AddError(
				"Error Removing Permission",
				fmt.Sprintf("Could not remove permission %s from role %s: %s", statePerm, plan.ID.ValueString(), err),
			)
			return
		}
	}

	if len(permsToAdd) > 0 {
		permissionItems := make([]kindeRolePermissionUpdate, len(permsToAdd))
		for i, permission := range permsToAdd {
			permissionItems[i] = kindeRolePermissionUpdate{ID: permission}
		}

		err = r.updateRolePermissions(ctx, plan.ID.ValueString(), permissionItems)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Adding Permissions",
				fmt.Sprintf("Could not add permissions to role: %s", err),
			)
			return
		}
	}

	// Get the updated role to ensure we have all fields and permissions
	role, err := r.getRole(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Role",
			fmt.Sprintf("Could not read updated role: %s", err),
		)
		return
	}

	updatedState, err := flattenRoleResource(ctx, role, role.Permissions)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}
	state = updatedState

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.deleteRole(ctx, state.ID.ValueString()); err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Role",
			fmt.Sprintf("Could not delete role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *RoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, err := r.getRole(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Kinde Role",
			"Could not read Kinde role ID "+req.ID+": "+err.Error(),
		)
		return
	}

	state, err := flattenRoleResource(ctx, role, role.Permissions)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			"Could not set role state: "+err.Error(),
		)
		return
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// getRole reads a role's core fields and fully paginated permission list,
// walking every permissions page via the shared getAllPages helper so roles
// with more permissions than fit on one page are not silently truncated.
func (r *RoleResource) getRole(ctx context.Context, id string) (*kindeRole, error) {
	var response struct {
		Role kindeRole `json:"role"`
	}
	if err := r.client.getJSON(ctx, "/api/v1/roles/"+url.PathEscape(id), &response); err != nil {
		return nil, err
	}

	permissionIDs, err := r.getRolePermissions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get role permissions: %w", err)
	}
	response.Role.Permissions = permissionIDs

	return &response.Role, nil
}

func (r *RoleResource) getRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	endpoint := fmt.Sprintf("/api/v1/roles/%s/permissions", url.PathEscape(roleID))
	permissions, err := getAllPages(ctx, func(ctx context.Context, nextToken string) ([]kindeRolePermission, string, error) {
		var page struct {
			NextToken   string                `json:"next_token"`
			Permissions []kindeRolePermission `json:"permissions"`
		}
		if err := r.client.getJSON(ctx, pagedPath(endpoint, nextToken), &page); err != nil {
			return nil, "", err
		}
		return page.Permissions, page.NextToken, nil
	})
	if err != nil {
		return nil, err
	}

	permissionIDs := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permissionIDs = append(permissionIDs, permission.ID)
	}

	return permissionIDs, nil
}

func (r *RoleResource) createRole(ctx context.Context, params management_api.CreateRoleReq) (*kindeRole, error) {
	res, err := r.client.CreateRole(ctx, management_api.NewOptCreateRoleReq(params))
	response, err := expectResponse[management_api.CreateRolesResponse]("create role", res, err)
	if err != nil {
		return nil, err
	}
	return &kindeRole{ID: response.Role.Or(management_api.CreateRolesResponseRole{}).ID.Or("")}, nil
}

func (r *RoleResource) deleteRole(ctx context.Context, roleID string) error {
	res, err := r.client.DeleteRole(ctx, management_api.DeleteRoleParams{RoleID: roleID})
	_, err = expectResponse[management_api.SuccessResponse]("delete role", res, err)
	return err
}

func (r *RoleResource) updateRolePermissions(ctx context.Context, roleID string, permissions []kindeRolePermissionUpdate) error {
	payload := struct {
		Permissions []kindeRolePermissionUpdate `json:"permissions"`
	}{Permissions: permissions}
	return r.client.doJSON(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/roles/%s/permissions", url.PathEscape(roleID)), payload, nil)
}

func (r *RoleResource) removeRolePermission(ctx context.Context, roleID, permissionID string) error {
	return r.client.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/roles/%s/permissions/%s", url.PathEscape(roleID), url.PathEscape(permissionID)), nil, nil)
}
