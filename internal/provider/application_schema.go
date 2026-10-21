// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/kinde-oss/terraform-provider-kinde/internal/serde"
)

type kindeApplication struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	LoginURI     string   `json:"login_uri"`
	HomepageURI  string   `json:"homepage_uri"`
	LogoutURIs   []string `json:"logout_uris"`
	RedirectURIs []string `json:"redirect_uris"`
}

type kindeApplicationUpdate struct {
	Name         string
	LogoutURIs   []string
	RedirectURIs []string
	LoginURI     string
	HomepageURI  string
}

func (u kindeApplicationUpdate) request() management_api.UpdateApplicationReq {
	var req management_api.UpdateApplicationReq
	if u.Name != "" {
		req.Name = management_api.NewOptString(u.Name)
	}
	if len(u.LogoutURIs) > 0 {
		req.LogoutUris = u.LogoutURIs
	}
	if len(u.RedirectURIs) > 0 {
		req.RedirectUris = u.RedirectURIs
	}
	if u.LoginURI != "" {
		req.LoginURI = management_api.NewOptString(u.LoginURI)
	}
	if u.HomepageURI != "" {
		req.HomepageURI = management_api.NewOptString(u.HomepageURI)
	}
	return req
}

func getKindeApplication(ctx context.Context, client *ManagementAPI, id string) (*kindeApplication, error) {
	var response struct {
		Application kindeApplication `json:"application"`
	}
	if err := client.getJSON(ctx, "/api/v1/applications/"+url.PathEscape(id), &response); err != nil {
		return nil, err
	}
	return &response.Application, nil
}

func createKindeApplication(ctx context.Context, client *ManagementAPI, name, appType string) (*kindeApplication, error) {
	res, err := client.CreateApplication(ctx, &management_api.CreateApplicationReq{
		Name: name,
		Type: management_api.CreateApplicationReqType(appType),
	})
	response, err := expectResponse[management_api.CreateApplicationResponse]("create application", res, err)
	if err != nil {
		return nil, err
	}

	created := response.Application.Or(management_api.CreateApplicationResponseApplication{})
	return &kindeApplication{
		ID:           created.ID.Or(""),
		ClientID:     created.ClientID.Or(""),
		ClientSecret: created.ClientSecret.Or(""),
	}, nil
}

func updateKindeApplication(ctx context.Context, client *ManagementAPI, id string, params kindeApplicationUpdate) error {
	res, err := client.UpdateApplication(ctx,
		management_api.NewOptUpdateApplicationReq(params.request()),
		management_api.UpdateApplicationParams{ApplicationID: id},
	)
	_, err = expectResponse[management_api.UpdateApplicationOK]("update application", res, err)
	return err
}

func deleteKindeApplication(ctx context.Context, client *ManagementAPI, id string) error {
	res, err := client.DeleteApplication(ctx, management_api.DeleteApplicationParams{ApplicationID: id})
	_, err = expectResponse[management_api.SuccessResponse]("delete application", res, err)
	return err
}

type ApplicationResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	LoginURI     types.String `tfsdk:"login_uri"`
	HomepageURI  types.String `tfsdk:"homepage_uri"`
	LogoutURIs   types.List   `tfsdk:"logout_uris"`
	RedirectURIs types.List   `tfsdk:"redirect_uris"`
}

//nolint:unused
func expandApplicationResourceModel(data ApplicationResourceModel) kindeApplication {
	return kindeApplication{
		ID:           data.ID.ValueString(),
		Name:         data.Name.ValueString(),
		Type:         data.Type.ValueString(),
		ClientID:     data.ClientID.ValueString(),
		ClientSecret: data.ClientSecret.ValueString(),
	}
}

//nolint:unused
func expandApplicationUpdateResourceModel(ctx context.Context, data ApplicationResourceModel) kindeApplicationUpdate {
	var logoutURIs []string
	if !data.LogoutURIs.IsNull() {
		data.LogoutURIs.ElementsAs(ctx, &logoutURIs, false)
	}

	var redirectURIs []string
	if !data.RedirectURIs.IsNull() {
		data.RedirectURIs.ElementsAs(ctx, &redirectURIs, false)
	}

	return kindeApplicationUpdate{
		Name:         data.Name.ValueString(),
		LoginURI:     data.LoginURI.ValueString(),
		HomepageURI:  data.HomepageURI.ValueString(),
		LogoutURIs:   logoutURIs,
		RedirectURIs: redirectURIs,
	}
}

//nolint:unused
func flattenApplicationResource(ctx context.Context, resource *kindeApplication, params kindeApplicationUpdate) (ApplicationResourceModel, diag.Diagnostics) {
	model := ApplicationResourceModel{
		ID:           types.StringValue(resource.ID),
		Name:         types.StringValue(resource.Name),
		Type:         types.StringValue(resource.Type),
		ClientID:     types.StringValue(resource.ClientID),
		ClientSecret: types.StringValue(resource.ClientSecret),
		LoginURI:     types.StringValue(params.LoginURI),
		HomepageURI:  types.StringValue(params.HomepageURI),
	}

	var diags, nestedDiags diag.Diagnostics

	model.LogoutURIs, nestedDiags = serde.FlattenStringList(ctx, params.LogoutURIs)
	diags.Append(nestedDiags...)

	model.RedirectURIs, nestedDiags = serde.FlattenStringList(ctx, params.RedirectURIs)
	diags.Append(nestedDiags...)

	return model, diags
}

type ApplicationDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

//nolint:unused
func expandApplicationDataSourceModel(model ApplicationDataSourceModel) *kindeApplication {
	return &kindeApplication{
		ID:           model.ID.ValueString(),
		Name:         model.Name.ValueString(),
		Type:         model.Type.ValueString(),
		ClientID:     model.ClientID.ValueString(),
		ClientSecret: model.ClientSecret.ValueString(),
	}
}

//nolint:unused
func flattenApplicationDataSource(resource *kindeApplication) ApplicationDataSourceModel {
	return ApplicationDataSourceModel{
		ID:           types.StringValue(resource.ID),
		Name:         types.StringValue(resource.Name),
		Type:         types.StringValue(resource.Type),
		ClientID:     types.StringValue(resource.ClientID),
		ClientSecret: types.StringValue(resource.ClientSecret),
	}
}
