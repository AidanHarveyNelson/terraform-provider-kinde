// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	_ resource.Resource                = &PermissionResource{}
	_ resource.ResourceWithImportState = &PermissionResource{}
)

func NewPermissionResource() resource.Resource {
	return &PermissionResource{}
}

type PermissionResource struct {
	client *ManagementAPI
}

func (r *PermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

func (r *PermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Permissions represent individual access rights that can be assigned to roles. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/permissions) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the permission",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the permission",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the permission",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the permission",
				Optional:            true,
			},
		},
	}
}

func (r *PermissionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createParams := expandPermissionCreateParams(plan)
	permission, err := r.createPermission(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Permission",
			fmt.Sprintf("Could not create permission: %s", err),
		)
		return
	}
	createdPermissionID := permission.ID

	plan.ID = types.StringValue(permission.ID)

	// After creation, search for the permission to get its full details
	permission, err = r.searchPermission(ctx, plan.Name.ValueString(), plan.Key.ValueString())
	if err != nil {
		if cleanupErr := r.cleanupPermissionOnCreateFailure(ctx, createdPermissionID); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"Permission Create Rollback Failed",
				fmt.Sprintf("Could not read created permission and automatic rollback also failed for permission ID %s: %s. Manual intervention may be required to delete the partially created permission.", createdPermissionID, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading Created Permission",
			fmt.Sprintf("Could not read created permission: %s", err),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// List all permissions to avoid dropping results on larger tenants
	perms, err := r.listAllPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	// First try to find by ID if we have one
	if !state.ID.IsNull() {
		for _, p := range perms {
			if p.ID == state.ID.ValueString() {
				state = flattenPermissionResource(&p)
				diags = resp.State.Set(ctx, &state)
				resp.Diagnostics.Append(diags...)
				return
			}
		}
	}

	// If we couldn't find by ID, try to find by name and key
	if !state.Name.IsNull() && !state.Key.IsNull() {
		for _, p := range perms {
			if p.Name == state.Name.ValueString() && p.Key == state.Key.ValueString() {
				state = flattenPermissionResource(&p)
				diags = resp.State.Set(ctx, &state)
				resp.Diagnostics.Append(diags...)
				return
			}
		}
	}

	// If we couldn't find the permission, remove it from state
	resp.State.RemoveResource(ctx)
}

func (r *PermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateParams := expandPermissionUpdateParams(plan)
	res, err := r.client.UpdatePermissions(ctx,
		management_api.NewOptUpdatePermissionsReq(updateParams),
		management_api.UpdatePermissionsParams{PermissionID: plan.ID.ValueString()},
	)
	if _, err = expectResponse[management_api.SuccessResponse]("update permission", res, err); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Permission",
			fmt.Sprintf("Could not update permission ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// After update, search for the permission to get its latest state
	permission, err := r.searchPermission(ctx, plan.Name.ValueString(), plan.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Permission",
			fmt.Sprintf("Could not read updated permission: %s", err),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.deletePermission(ctx, state.ID.ValueString()); err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Permission",
			fmt.Sprintf("Could not delete permission ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *PermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// List all permissions to reduce API churn while respecting pagination
	perms, err := r.listAllPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	// Find the permission by ID
	for _, p := range perms {
		if p.ID == req.ID {
			state := flattenPermissionResource(&p)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.Diagnostics.AddError(
		"Error Reading Permission",
		fmt.Sprintf("Could not find permission with ID %s", req.ID),
	)
}

func (r *PermissionResource) listAllPermissions(ctx context.Context) ([]kindePermission, error) {
	return getAllPages(ctx, func(ctx context.Context, nextToken string) ([]kindePermission, string, error) {
		var page struct {
			NextToken   string            `json:"next_token"`
			Permissions []kindePermission `json:"permissions"`
		}
		if err := r.client.getJSON(ctx, pagedPath("/api/v1/permissions", nextToken), &page); err != nil {
			return nil, "", err
		}
		return page.Permissions, page.NextToken, nil
	})
}

func (r *PermissionResource) searchPermission(ctx context.Context, name, key string) (*kindePermission, error) {
	permissions, err := r.listAllPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find permission with name %s and key %s: %w", name, key, err)
	}

	for _, permission := range permissions {
		if permission.Name == name && permission.Key == key {
			return &permission, nil
		}
	}

	return nil, fmt.Errorf("could not find permission with name %s and key %s: permission not found", name, key)
}

func (r *PermissionResource) createPermission(ctx context.Context, params permissionCreateParams) (*kindePermission, error) {
	var response struct {
		Permission struct {
			ID string `json:"id"`
		} `json:"permission"`
	}
	if err := r.client.doJSON(ctx, http.MethodPost, "/api/v1/permissions", params, &response); err != nil {
		return nil, err
	}
	return &kindePermission{ID: response.Permission.ID}, nil
}

func (r *PermissionResource) deletePermission(ctx context.Context, permissionID string) error {
	res, err := r.client.DeletePermission(ctx, management_api.DeletePermissionParams{PermissionID: permissionID})
	_, err = expectResponse[management_api.SuccessResponse]("delete permission", res, err)
	return err
}

func (r *PermissionResource) cleanupPermissionOnCreateFailure(ctx context.Context, permissionID string) error {
	if permissionID == "" {
		return nil
	}

	if err := r.deletePermission(ctx, permissionID); err != nil && !isNotFoundError(err) {
		return err
	}

	return nil
}
