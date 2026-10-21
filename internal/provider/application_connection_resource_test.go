// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplicationConnectionResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckKindeApplicationConnectionsDestroyed,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccApplicationConnectionResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "application_id", "kinde_application.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "connection_id", "kinde_connection.test", "id"),
					testAccCheckKindeApplicationConnection("kinde_application_connection.test"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_application_connection.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccApplicationConnectionResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_application" "test" {
	name = %[1]q
	type = "reg"
}

resource "kinde_connection" "test" {
	name         = %[1]q
	display_name = "Test OAuth2 Connection"
	strategy     = "oauth2:google"
	options = {
		client_id     = "test-client-id"
		client_secret = "test-client-secret"
	}
}

resource "kinde_application_connection" "test" {
	application_id = kinde_application.test.id
	connection_id  = kinde_connection.test.id
}
`, name)
}

func TestApplicationConnectionResourceOffline(t *testing.T) {
	fake := newFakeKinde(t)
	config := testAccApplicationConnectionResourceConfig("offline")

	linked := func(state *terraformState) (bool, error) {
		appID, err := stateAttr(state, "kinde_application_connection.test", "application_id")
		if err != nil {
			return false, err
		}
		connectionID, err := stateAttr(state, "kinde_application_connection.test", "connection_id")
		if err != nil {
			return false, err
		}
		var enabled bool
		fake.do(func() { enabled = fake.appConnections[appID][connectionID] })
		return enabled, nil
	}
	expectLinked := func(state *terraformState) error {
		enabled, err := linked(state)
		if err == nil && !enabled {
			err = fmt.Errorf("connection is not enabled for the application in Kinde")
		}
		return err
	}

	var linkID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: fake.check(func() error {
			if !fake.disabledLinks[linkID] {
				return fmt.Errorf("connection %s was not disabled in Kinde on destroy", linkID)
			}
			return nil
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "application_id", "kinde_application.test", "id"),
					expectLinked,
					captureAttr("kinde_application_connection.test", "id", &linkID),
				),
			},
			{
				ResourceName:      "kinde_application_connection.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			offlineNotFoundStep(config, func() {
				fake.do(func() {
					for appID := range fake.appConnections {
						fake.appConnections[appID] = map[string]bool{}
					}
				})
			}),
			{
				Config: config,
				Check:  expectLinked,
			},
		},
	})
}

func TestApplicationResourceOffline_Connections(t *testing.T) {
	fake := newFakeKinde(t)
	fake.do(func() {
		id := fake.newID("conn")
		fake.connections[id] = &fakeConnection{ID: id, Name: "custom", DisplayName: "Custom", Strategy: "oauth2:google"}
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApplicationResourceConfig_WithConnections("offline") + `
data "kinde_connections" "all" {}

data "kinde_connections" "custom" {
  filter = "custom"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_connections.builtin", "connections.#", "5"),
					resource.TestCheckResourceAttr("data.kinde_connections.all", "connections.#", "6"),
					resource.TestCheckResourceAttr("data.kinde_connections.custom", "connections.#", "1"),
					resource.TestCheckResourceAttr("data.kinde_connections.custom", "connections.0.strategy", "oauth2:google"),
					func(state *terraformState) error {
						appID, err := stateAttr(state, "kinde_application.test", "id")
						if err != nil {
							return err
						}
						return fake.check(func() error {
							var strategies []string
							for connectionID := range fake.appConnections[appID] {
								strategies = append(strategies, fake.connections[connectionID].Strategy)
							}
							sort.Strings(strategies)
							if fmt.Sprint(strategies) != "[username:otp username:password]" {
								return fmt.Errorf("unexpected connections enabled in Kinde: %v", strategies)
							}
							return nil
						})(state)
					},
				),
			},
		},
	})
}
