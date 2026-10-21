// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplicationResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	uri := fmt.Sprintf("http://localhost:%d", acctest.RandIntRange(3000, 4000))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeApplicationsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "kinde_application" "test" {
					name          = "%[1]s"
					type          = "reg"
					login_uri     = "%[2]s/oauth/login"
					homepage_uri  = "%[2]s"
					logout_uris   = ["%[2]s/oauth/logout"]
					redirect_uris = ["%[2]s/oauth/redirect"]
				}
				`, testID, uri),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttr("kinde_application.test", "login_uri", uri+"/oauth/login"),
					resource.TestCheckResourceAttr("kinde_application.test", "homepage_uri", uri),
					resource.TestCheckResourceAttr("kinde_application.test", "logout_uris.0", uri+"/oauth/logout"),
					resource.TestCheckResourceAttr("kinde_application.test", "redirect_uris.0", uri+"/oauth/redirect"),
					testAccCheckKindeApplication("kinde_application.test", testID, "reg"),
				),
			},
			{
				ResourceName:      "kinde_application.test",
				ImportState:       true,
				ImportStateVerify: true,
				// the api does not return these values
				ImportStateVerifyIgnore: []string{
					"homepage_uri",
					"login_uri",
					"logout_uris",
					"redirect_uris",
				},
			},
		},
	})
}

func TestAccApplicationResource_Connections(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckKindeApplicationsDestroyed,
			testAccCheckKindeApplicationConnectionsDestroyed,
		),
		Steps: []resource.TestStep{
			{
				Config: testAccApplicationResourceConfig_WithConnections(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					// Check that both connections exist and are linked
					resource.TestCheckResourceAttrSet("kinde_application_connection.password", "id"),
					resource.TestCheckResourceAttrSet("kinde_application_connection.otp", "id"),
					// Verify the application IDs match
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.password", "application_id",
						"kinde_application.test", "id",
					),
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.otp", "application_id",
						"kinde_application.test", "id",
					),
					testAccCheckKindeApplication("kinde_application.test", testID, "reg"),
					testAccCheckKindeApplicationConnection("kinde_application_connection.password"),
					testAccCheckKindeApplicationConnection("kinde_application_connection.otp"),
				),
			},
		},
	})
}

func testAccApplicationResourceConfig_WithConnections(name string) string {
	return fmt.Sprintf(`
data "kinde_connections" "builtin" {
	filter = "builtin"
}

resource "kinde_application" "test" {
	name = %[1]q
	type = "reg"
}

resource "kinde_application_connection" "password" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:password")].id
}

resource "kinde_application_connection" "otp" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:otp")].id
}
`, name)
}

func testApplicationOfflineConfig(name, uri string) string {
	return fmt.Sprintf(`
resource "kinde_application" "test" {
  name          = %[1]q
  type          = "reg"
  login_uri     = "%[2]s/login"
  homepage_uri  = %[2]q
  logout_uris   = ["%[2]s/logout"]
  redirect_uris = ["%[2]s/callback"]
}
`, name, uri)
}

func TestApplicationResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	first := testApplicationOfflineConfig("app", "https://first.example.com")
	updated := testApplicationOfflineConfig("app", "https://updated.example.com")

	expectApplication := func(uri string) resource.TestCheckFunc {
		return func(state *terraformState) error {
			id, err := stateAttr(state, "kinde_application.test", "id")
			if err != nil {
				return err
			}
			return fake.check(func() error {
				app, ok := fake.applications[id]
				if !ok {
					return fmt.Errorf("application %s not found in Kinde", id)
				}
				if app.Name != "app" || app.Type != "reg" || app.LoginURI != uri+"/login" || app.HomepageURI != uri ||
					len(app.LogoutURIs) != 1 || app.LogoutURIs[0] != uri+"/logout" ||
					len(app.RedirectURIs) != 1 || app.RedirectURIs[0] != uri+"/callback" {
					return fmt.Errorf("unexpected application in Kinde: %+v", *app)
				}
				return nil
			})(state)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if len(fake.applications) != 0 {
				return fmt.Errorf("%d applications still exist", len(fake.applications))
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					expectApplication("https://first.example.com"),
				),
			},
			{
				ResourceName:            "kinde_application.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"logout_uris", "redirect_uris"},
			},
			{
				Config: updated,
				Check:  expectApplication("https://updated.example.com"),
			},
			offlineNotFoundStep(updated, func() {
				fake.do(func() {
					for id := range fake.applications {
						delete(fake.applications, id)
					}
				})
			}),
			{
				Config: updated,
				Check:  expectApplication("https://updated.example.com"),
			},
		},
	})
}
