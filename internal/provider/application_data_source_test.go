// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplicationDataSource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "kinde_application" "test" {
					name = %[1]q
					type = "reg"
				}

				data "kinde_application" "test" {
					id = kinde_application.test.id
				}
				`, testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.kinde_application.test", "id"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("data.kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttrPair("data.kinde_application.test", "client_id", "kinde_application.test", "client_id"),
					testAccCheckKindeApplication("kinde_application.test", testID, "reg"),
				),
			},
		},
	})
}

func TestApplicationDataSourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	fake.do(func() {
		fake.applications["app_existing"] = &fakeApplication{ID: "app_existing", Name: "Existing", Type: "spa", ClientID: "existing_client", ClientSecret: "existing_secret"}
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_application" "test" { id = "app_existing" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_application.test", "name", "Existing"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "type", "spa"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "client_id", "existing_client"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "client_secret", "existing_secret"),
				),
			},
			{
				Config:      `data "kinde_application" "test" { id = "app_missing" }`,
				ExpectError: regexp.MustCompile(`INVALID_APP_ID`),
			},
		},
	})
}
