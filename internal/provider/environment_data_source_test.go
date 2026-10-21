// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

func TestAccEnvironmentDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_environment" "test" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "code"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "name"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "is_default"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "kinde_domain"),
				),
			},
		},
	})
}

func TestEnvironmentDataSourceAgainstStubServer(t *testing.T) {
	server := newStubManagementServer(t, map[string]http.HandlerFunc{
		"/api/v1/environment": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "OK",
				"message": "success_response",
				"environment": map[string]any{
					"code":          "production",
					"name":          "Production",
					"is_default":    true,
					"is_live":       true,
					"kinde_domain":  "example.kinde.com",
					"custom_domain": nil,
					"theme_code":    "user_preference",
					"created_on":    "2026-01-01T00:00:00+00:00",
				},
			})
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_environment" "test" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_environment.test", "code", "production"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "name", "Production"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "is_default", "true"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "is_live", "true"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "kinde_domain", "example.kinde.com"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "theme_code", "user_preference"),
					resource.TestCheckNoResourceAttr("data.kinde_environment.test", "custom_domain"),
				),
			},
		},
	})

	if got := server.requests("/api/v1/environment"); got == 0 {
		t.Fatal("expected the data source to call /api/v1/environment")
	}
}

func TestEnvironmentDataSourceReportsAPIErrors(t *testing.T) {
	newStubManagementServer(t, map[string]http.HandlerFunc{
		"/api/v1/environment": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"code": "FORBIDDEN", "message": "Missing scope read:environments"}},
			})
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `data "kinde_environment" "test" {}`,
				ExpectError: regexp.MustCompile(`FORBIDDEN:\s+Missing scope`),
			},
		},
	})
}

func TestManagementAPIError(t *testing.T) {
	res := &management_api.GetEnvironmentBadRequest{
		Errors: []management_api.Error{
			{Code: management_api.NewOptString("INVALID"), Message: management_api.NewOptString("Bad input")},
		},
	}

	err := managementAPIError("get environment", res)
	if !strings.Contains(err.Error(), "INVALID: Bad input") {
		t.Fatalf("expected error to include the API error, got %q", err)
	}

	err = managementAPIError("get environment", &management_api.GetEnvironmentResponse{})
	if !strings.Contains(err.Error(), "unexpected response") {
		t.Fatalf("expected an unexpected response error, got %q", err)
	}
}
