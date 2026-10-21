// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestKindeProviderResourcesSurface(t *testing.T) {
	provider := &KindeProvider{}

	resources := provider.Resources(context.Background())
	got := make([]string, 0, len(resources))
	for _, factory := range resources {
		instance := factory()
		var resp resource.MetadataResponse
		instance.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "kinde"}, &resp)
		got = append(got, resp.TypeName)
	}

	want := []string{
		"kinde_api",
		"kinde_application",
		"kinde_application_connection",
		"kinde_connection",
		"kinde_organization",
		"kinde_organization_user",
		"kinde_role",
		"kinde_user",
		"kinde_permission",
		"kinde_user_role",
		"kinde_environment_feature_flags",
		"kinde_feature_flag",
		"kinde_organization_feature_flags",
		"kinde_user_feature_flags",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected resources surface: want %v, got %v", want, got)
	}
}

func TestKindeProviderDataSourcesSurface(t *testing.T) {
	provider := &KindeProvider{}

	dataSources := provider.DataSources(context.Background())
	got := make([]string, 0, len(dataSources))
	for _, factory := range dataSources {
		instance := factory()
		var resp datasource.MetadataResponse
		instance.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "kinde"}, &resp)
		got = append(got, resp.TypeName)
	}

	want := []string{
		"kinde_api",
		"kinde_application",
		"kinde_connections",
		"kinde_environment",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected data sources surface: want %v, got %v", want, got)
	}
}
