// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	config := testAccAPIResourceConfig(testID, testID)
	var apiID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeAPIsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_api.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_api.test", "audience", testID),
					testAccCheckKindeAPI("kinde_api.test", testID, testID),
					captureAttr("kinde_api.test", "id", &apiID),
				),
			},
			{
				ResourceName:      "kinde_api.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				PreConfig: testAccDeleteOutsideTerraform(t, "API", func(ctx context.Context, client *ManagementAPI) error {
					return (&APIResource{client: client}).deleteAPI(ctx, apiID)
				}),
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  testAccCheckKindeAPI("kinde_api.test", testID, testID),
			},
		},
	})
}

func testAccAPIResourceConfig(name, audience string) string {
	return fmt.Sprintf(`
resource "kinde_api" "test" {
  name     = %[1]q
  audience = %[2]q
}
`, name, audience)
}

func TestAPIResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	first := testAccAPIResourceConfig("first", "https://first.example.com")
	replaced := testAccAPIResourceConfig("replaced", "https://replaced.example.com")
	var firstID string

	expectAPI := func(name, audience string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			id, err := stateAttr(state, "kinde_api.test", "id")
			if err != nil {
				return err
			}
			return fake.check(func() error {
				api, ok := fake.apis[id]
				if !ok {
					return fmt.Errorf("API %s not found in Kinde", id)
				}
				if api.Name != name || api.Audience != audience {
					return fmt.Errorf("unexpected API in Kinde: %+v", *api)
				}
				return nil
			})(state)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if len(fake.apis) != 0 {
				return fmt.Errorf("%d APIs still exist", len(fake.apis))
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_api.test", "name", "first"),
					resource.TestCheckResourceAttr("kinde_api.test", "is_management_api", "false"),
					expectAPI("first", "https://first.example.com"),
					captureAttr("kinde_api.test", "id", &firstID),
				),
			},
			{
				ResourceName:      "kinde_api.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: replaced,
				Check: resource.ComposeAggregateTestCheckFunc(
					expectAPI("replaced", "https://replaced.example.com"),
					fake.check(func() error {
						if _, ok := fake.apis[firstID]; ok {
							return fmt.Errorf("replaced API %s still exists", firstID)
						}
						return nil
					}),
				),
			},
			offlineNotFoundStep(replaced, func() {
				fake.do(func() {
					for id := range fake.apis {
						delete(fake.apis, id)
					}
				})
			}),
			{
				Config: replaced,
				Check:  expectAPI("replaced", "https://replaced.example.com"),
			},
		},
	})
}
