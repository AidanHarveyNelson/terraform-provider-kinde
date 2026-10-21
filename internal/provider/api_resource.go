// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	_ resource.Resource                = &APIResource{}
	_ resource.ResourceWithImportState = &APIResource{}
)

func NewAPIResource() resource.Resource {
	return &APIResource{}
}

type APIResource struct {
	client *ManagementAPI
}

func (r *APIResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (r *APIResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"is_management_api": schema.BoolAttribute{
				MarkdownDescription: "Whether this API is a management API",
				Computed:            true,
			},
		},
	}
}

func (r *APIResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *APIResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan APIResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	api := expandAPIResourceModel(plan)
	res, err := r.client.AddAPIs(ctx, &management_api.AddAPIsReq{
		Name:     api.Name,
		Audience: api.Audience,
	})
	created, err := expectResponse[management_api.CreateApisResponse]("create API", res, err)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating API",
			fmt.Sprintf("Could not create API: %s", err),
		)
		return
	}

	createdAPI := kindeAPI{ID: created.API.Or(management_api.CreateApisResponseAPI{}).ID.Or("")}

	// Get the created API to populate computed fields
	api, err = getKindeAPI(ctx, r.client, createdAPI.ID)
	if err != nil {
		if cleanupErr := r.cleanupAPIOnCreateFailure(ctx, createdAPI.ID); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"API Create Rollback Failed",
				fmt.Sprintf("Could not read created API and automatic rollback also failed for API ID %s: %s. Manual intervention may be required to delete the partially created API.", createdAPI.ID, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", createdAPI.ID, err),
		)
		return
	}

	state := flattenAPIResource(api)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

// cleanupAPIOnCreateFailure best-effort deletes an API that was successfully
// created but could not be fully read back, so that a failed Create does not
// leave an orphaned API that Terraform state never tracks (and that
// `terraform destroy` therefore cannot clean up).
func (r *APIResource) cleanupAPIOnCreateFailure(ctx context.Context, apiID string) error {
	if apiID == "" {
		return nil
	}

	if err := r.deleteAPI(ctx, apiID); err != nil && !isNotFoundError(err) {
		tflog.Warn(ctx, "Failed to clean up API after create failure", map[string]interface{}{
			"id":    apiID,
			"error": err.Error(),
		})
		return err
	}

	return nil
}

func (r *APIResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state APIResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	api, err := getKindeAPI(ctx, r.client, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state = flattenAPIResource(api)
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *APIResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"API Update Not Supported",
		"The Kinde API does not support updating APIs. To change the configuration, you must create a new API.",
	)
}

func (r *APIResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state APIResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.deleteAPI(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting API",
			fmt.Sprintf("Could not delete API ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *APIResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *APIResource) deleteAPI(ctx context.Context, apiID string) error {
	res, err := r.client.DeleteAPI(ctx, management_api.DeleteAPIParams{APIID: apiID})
	_, err = expectResponse[management_api.DeleteAPIResponse]("delete API", res, err)
	return err
}
