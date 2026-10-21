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

func TestAccAPIDataSource(t *testing.T) {
	// Audiences must be unique within a Kinde business.
	testID := acctest.RandomWithPrefix("tfacc-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccAPIDataSourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_api.test", "name", testID),
					resource.TestCheckResourceAttr("data.kinde_api.test", "audience", testID),
					testAccCheckKindeAPI("kinde_api.test", testID, testID),
				),
			},
		},
	})
}

func testAccAPIDataSourceConfig(testID string) string {
	return fmt.Sprintf(`
resource "kinde_api" "test" {
	name     = %[1]q
	audience = %[1]q
}

data "kinde_api" "test" {
	id = kinde_api.test.id
}
`, testID)
}

func TestAPIDataSourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	fake.do(func() {
		fake.apis["api_existing"] = &fakeAPI{ID: "api_existing", Name: "Existing", Audience: "https://existing.example.com"}
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_api" "test" { id = "api_existing" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_api.test", "name", "Existing"),
					resource.TestCheckResourceAttr("data.kinde_api.test", "audience", "https://existing.example.com"),
				),
			},
			{
				Config:      `data "kinde_api" "test" { id = "api_missing" }`,
				ExpectError: regexp.MustCompile(`API_INVALID`),
			},
		},
	})
}
