// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/permissions"
)

var (
	_ resource.Resource                = &PermissionResource{}
	_ resource.ResourceWithImportState = &PermissionResource{}
)

func NewPermissionResource() resource.Resource {
	return &PermissionResource{}
}

type PermissionResource struct {
	client *permissions.Client
}

// permissionsPage adapts a raw /api/v1/permissions response page to the
// shared getAllPages pagination helper.
type permissionsPage struct {
	Code        string                   `json:"code"`
	Message     string                   `json:"message"`
	NextToken   string                   `json:"next_token"`
	Permissions []permissions.Permission `json:"permissions"`
}

func (p permissionsPage) getData() []permissions.Permission { return p.Permissions }

func (p permissionsPage) getNextToken() string { return p.NextToken }

// Compile-time assertion that permissionsPage satisfies kindePage. This also
// keeps static analysis tools that don't fully trace generic instantiations
// (e.g. getAllPages[permissions.Permission, permissionsPage]) from flagging
// getData/getNextToken as unused.
var _ kindePage[permissions.Permission] = permissionsPage{}

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

	client, ok := req.ProviderData.(*KindeClients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *KindeClients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client.Permissions
}

func (r *PermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createParams := expandPermissionCreateParams(plan)
	permission, err := r.client.Create(ctx, createParams)
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
	searchParams := permissions.SearchParams{
		Name: plan.Name.ValueString(),
		Key:  plan.Key.ValueString(),
	}

	permission, err = r.client.Search(ctx, searchParams)
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
	err := r.client.Update(ctx, plan.ID.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Permission",
			fmt.Sprintf("Could not update permission ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// After update, search for the permission to get its latest state
	searchParams := permissions.SearchParams{
		Name: plan.Name.ValueString(),
		Key:  plan.Key.ValueString(),
	}

	permission, err := r.client.Search(ctx, searchParams)
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

	if err := r.client.Delete(ctx, state.ID.ValueString()); err != nil {
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

func (r *PermissionResource) listAllPermissions(ctx context.Context) ([]permissions.Permission, error) {
	return getAllPages[permissions.Permission, permissionsPage](ctx, r.client, "/api/v1/permissions", nil)
}

func (r *PermissionResource) cleanupPermissionOnCreateFailure(ctx context.Context, permissionID string) error {
	if permissionID == "" {
		return nil
	}

	if err := r.client.Delete(ctx, permissionID); err != nil && !isNotFoundError(err) {
		return err
	}

	return nil
}
