// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

type OrganizationResource struct {
	client *ManagementAPI
}

type OrganizationResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Code            types.String `tfsdk:"code"`
	Name            types.String `tfsdk:"name"`
	ExternalID      types.String `tfsdk:"external_id"`
	BackgroundColor types.String `tfsdk:"background_color"`
	ButtonColor     types.String `tfsdk:"button_color"`
	ButtonTextColor types.String `tfsdk:"button_text_color"`
	LinkColor       types.String `tfsdk:"link_color"`
	ThemeCode       types.String `tfsdk:"theme_code"`
	Handle          types.String `tfsdk:"handle"`
	CreatedOn       types.String `tfsdk:"created_on"`
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Description: "The organization code.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the organization.",
				Required:    true,
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID of the organization.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"background_color": schema.StringAttribute{
				Description: "The background color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_color": schema.StringAttribute{
				Description: "The button color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_text_color": schema.StringAttribute{
				Description: "The button text color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				Description: "The link color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"theme_code": schema.StringAttribute{
				Description: "The theme code of the organization.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"handle": schema.StringAttribute{
				Description: "The organization handle.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_on": schema.StringAttribute{
				Description: "The timestamp when the organization was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createParams := kindeOrganizationCreate{
		Name: plan.Name.ValueString(),
	}

	if handle, ok := stringValueIfSet(plan.Handle); ok {
		createParams.Handle = handle
	}

	organization, err := r.createOrganization(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			fmt.Sprintf("Could not create organization: %s", err),
		)
		return
	}

	// Capture the code up front: on error, getOrganization/updateOrganization
	// return a nil *kindeOrganization, so organization.Code cannot be safely
	// dereferenced from their result once an error path is taken.
	code := organization.Code

	// Get the created organization to ensure we have all fields
	organization, err = r.getOrganization(ctx, code)
	if err != nil {
		if cleanupErr := r.cleanupOrganizationOnCreateFailure(ctx, code); cleanupErr != nil {
			resp.Diagnostics.AddWarning(
				"Organization Create Rollback Failed",
				fmt.Sprintf("Could not read created organization and automatic rollback also failed for organization code %s: %s. Manual intervention may be required to delete the partially created organization.", code, cleanupErr),
			)
		}
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", code, err),
		)
		return
	}

	// Apply optional attributes immediately after creation when requested.
	if updateParams, needsUpdate := buildOrganizationUpdateParams(plan); needsUpdate {
		organization, err = r.updateOrganization(ctx, code, updateParams)
		if err != nil {
			if cleanupErr := r.cleanupOrganizationOnCreateFailure(ctx, code); cleanupErr != nil {
				resp.Diagnostics.AddWarning(
					"Organization Create Rollback Failed",
					fmt.Sprintf("Could not update created organization and automatic rollback also failed for organization code %s: %s. Manual intervention may be required to delete the partially created organization.", code, cleanupErr),
				)
			}
			resp.Diagnostics.AddError(
				"Error Updating Organization",
				fmt.Sprintf("Could not update organization code %s after create: %s", code, err),
			)
			return
		}
	}

	mapOrganizationToState(organization, &plan, createParams.Handle)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// cleanupOrganizationOnCreateFailure best-effort deletes an organization that
// was successfully created but could not be fully configured, so that a
// failed Create does not leave an orphaned organization that Terraform state
// never tracks (and that `terraform destroy` therefore cannot clean up).
func (r *OrganizationResource) cleanupOrganizationOnCreateFailure(ctx context.Context, code string) error {
	if code == "" {
		return nil
	}

	if err := r.deleteOrganization(ctx, code); err != nil && !isNotFoundError(err) {
		tflog.Warn(ctx, "Failed to clean up organization after create failure", map[string]interface{}{
			"code":  code,
			"error": err.Error(),
		})
		return err
	}

	return nil
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization, err := r.getOrganization(ctx, state.Code.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}

	mapOrganizationToState(organization, &state, "")

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateParams := kindeOrganizationUpdate{
		Name:            plan.Name.ValueString(),
		ExternalID:      plan.ExternalID.ValueString(),
		BackgroundColor: plan.BackgroundColor.ValueString(),
		ButtonColor:     plan.ButtonColor.ValueString(),
		ButtonTextColor: plan.ButtonTextColor.ValueString(),
		LinkColor:       plan.LinkColor.ValueString(),
		ThemeCode:       plan.ThemeCode.ValueString(),
		Handle:          plan.Handle.ValueString(),
	}

	organization, err := r.updateOrganization(ctx, plan.Code.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Organization",
			fmt.Sprintf("Could not update organization code %s: %s", plan.Code.ValueString(), err),
		)
		return
	}

	mapOrganizationToState(organization, &plan, "")

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.deleteOrganization(ctx, state.Code.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Organization",
			fmt.Sprintf("Could not delete organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Get the organization by code
	organization, err := r.getOrganization(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", req.ID, err),
		)
		return
	}

	// Create a new state
	var state OrganizationResourceModel

	mapOrganizationToState(organization, &state, "")

	// Set the state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func buildOrganizationUpdateParams(plan OrganizationResourceModel) (kindeOrganizationUpdate, bool) {
	params := kindeOrganizationUpdate{
		Name: plan.Name.ValueString(),
	}

	var needsUpdate bool

	if value, ok := stringValueIfSet(plan.ExternalID); ok {
		params.ExternalID = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.BackgroundColor); ok {
		params.BackgroundColor = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.ButtonColor); ok {
		params.ButtonColor = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.ButtonTextColor); ok {
		params.ButtonTextColor = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.LinkColor); ok {
		params.LinkColor = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.ThemeCode); ok {
		params.ThemeCode = value
		needsUpdate = true
	}
	if value, ok := stringValueIfSet(plan.Handle); ok {
		params.Handle = value
		needsUpdate = true
	}

	return params, needsUpdate
}

func mapOrganizationToState(org *kindeOrganization, model *OrganizationResourceModel, fallbackHandle string) {
	model.ID = types.StringValue(org.Code)
	model.Code = types.StringValue(org.Code)
	model.Name = types.StringValue(org.Name)
	model.CreatedOn = types.StringValue(org.CreatedOn.Format(time.RFC3339))
	model.ThemeCode = types.StringValue(org.ColorScheme)

	if org.Handle != nil {
		model.Handle = types.StringValue(*org.Handle)
	} else if fallbackHandle != "" {
		model.Handle = types.StringValue(fallbackHandle)
	} else {
		model.Handle = types.StringNull()
	}

	if org.ExternalID != nil {
		model.ExternalID = types.StringValue(*org.ExternalID)
	} else {
		model.ExternalID = types.StringNull()
	}

	if org.BackgroundColor != nil {
		model.BackgroundColor = types.StringValue(org.BackgroundColor.Hex)
	} else {
		model.BackgroundColor = types.StringNull()
	}

	if org.ButtonColor != nil {
		model.ButtonColor = types.StringValue(org.ButtonColor.Hex)
	} else {
		model.ButtonColor = types.StringNull()
	}

	if org.ButtonTextColor != nil {
		model.ButtonTextColor = types.StringValue(org.ButtonTextColor.Hex)
	} else {
		model.ButtonTextColor = types.StringNull()
	}

	if org.LinkColor != nil {
		model.LinkColor = types.StringValue(org.LinkColor.Hex)
	} else {
		model.LinkColor = types.StringNull()
	}
}

func stringValueIfSet(value types.String) (string, bool) {
	if value.IsNull() || value.IsUnknown() {
		return "", false
	}
	return value.ValueString(), true
}

type kindeColor struct {
	Raw string `json:"raw"`
	Hex string `json:"hex"`
	Hsl string `json:"hsl"`
}

type kindeOrganization struct {
	Code            string      `json:"code"`
	Name            string      `json:"name"`
	Handle          *string     `json:"handle,omitempty"`
	CreatedOn       time.Time   `json:"created_on,omitempty"`
	LinkColor       *kindeColor `json:"link_color,omitempty"`
	ExternalID      *string     `json:"external_id,omitempty"`
	ButtonColor     *kindeColor `json:"button_color,omitempty"`
	ColorScheme     string      `json:"color_scheme"`
	BackgroundColor *kindeColor `json:"background_color,omitempty"`
	ButtonTextColor *kindeColor `json:"button_text_color,omitempty"`
}

type kindeOrganizationCreate struct {
	Name   string
	Handle string
}

type kindeOrganizationUpdate struct {
	Name            string
	ExternalID      string
	BackgroundColor string
	ButtonColor     string
	ButtonTextColor string
	LinkColor       string
	ThemeCode       string
	Handle          string
}

func optStringIfSet(value string) management_api.OptString {
	if value == "" {
		return management_api.OptString{}
	}
	return management_api.NewOptString(value)
}

func (r *OrganizationResource) createOrganization(ctx context.Context, params kindeOrganizationCreate) (*kindeOrganization, error) {
	res, err := r.client.CreateOrganization(ctx, &management_api.CreateOrganizationReq{
		Name:   params.Name,
		Handle: optStringIfSet(params.Handle),
	})
	response, err := expectResponse[management_api.CreateOrganizationResponse]("create organization", res, err)
	if err != nil {
		return nil, err
	}

	created := response.Organization.Or(management_api.CreateOrganizationResponseOrganization{})
	return &kindeOrganization{Code: created.Code.Or("")}, nil
}

func (r *OrganizationResource) getOrganization(ctx context.Context, code string) (*kindeOrganization, error) {
	var organization kindeOrganization
	if err := r.client.getJSON(ctx, "/api/v1/organization?code="+url.QueryEscape(code), &organization); err != nil {
		return nil, err
	}
	return &organization, nil
}

func (r *OrganizationResource) updateOrganization(ctx context.Context, code string, params kindeOrganizationUpdate) (*kindeOrganization, error) {
	req := management_api.UpdateOrganizationReq{
		Name:            optStringIfSet(params.Name),
		ExternalID:      optStringIfSet(params.ExternalID),
		BackgroundColor: optStringIfSet(params.BackgroundColor),
		ButtonColor:     optStringIfSet(params.ButtonColor),
		ButtonTextColor: optStringIfSet(params.ButtonTextColor),
		LinkColor:       optStringIfSet(params.LinkColor),
		Handle:          optStringIfSet(params.Handle),
	}
	if params.ThemeCode != "" {
		req.ThemeCode = management_api.NewOptUpdateOrganizationReqThemeCode(management_api.UpdateOrganizationReqThemeCode(params.ThemeCode))
	}

	res, err := r.client.UpdateOrganization(ctx,
		management_api.NewOptUpdateOrganizationReq(req),
		management_api.UpdateOrganizationParams{OrgCode: code},
	)
	if _, err := expectResponse[management_api.SuccessResponse]("update organization", res, err); err != nil {
		return nil, err
	}

	return r.getOrganization(ctx, code)
}

func (r *OrganizationResource) deleteOrganization(ctx context.Context, code string) error {
	res, err := r.client.DeleteOrganization(ctx, management_api.DeleteOrganizationParams{OrgCode: code})
	_, err = expectResponse[management_api.SuccessResponse]("delete organization", res, err)
	return err
}
