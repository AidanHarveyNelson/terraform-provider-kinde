// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/nxt-fwd/kinde-go"
)

func TestAccFeatureFlagResource(t *testing.T) {
	key := strings.ReplaceAll(acctest.RandomWithPrefix("tfacc_flag"), "-", "_")
	newKey := key + "_renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckFeatureFlagGone(key),
			testAccCheckFeatureFlagGone(newKey),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccFeatureFlagConfig(key, "Beta access", "bool", "org", "false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "id", key),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "key", key),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "type", "bool"),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "default_value", "false"),
				),
			},
			{
				Config: testAccFeatureFlagConfig(key, "Beta access v2", "bool", "usr", "true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "name", "Beta access v2"),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "allow_override_level", "usr"),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "default_value", "true"),
				),
			},
			{
				Config: testAccFeatureFlagConfig(key, "Beta access v2", "int", "org", "5"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "type", "int"),
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "default_value", "5"),
				),
			},
			{
				ResourceName:            "kinde_feature_flag.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"name", "description", "allow_override_level"},
			},
			{
				Config: testAccFeatureFlagConfig(newKey, "Beta access v2", "int", "org", "5"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "id", newKey),
					testAccCheckFeatureFlagGone(key),
				),
			},
		},
	})
}

func testAccFeatureFlagConfig(key, name, flagType, level, defaultValue string) string {
	return fmt.Sprintf(`
resource "kinde_feature_flag" "test" {
  key                  = %[1]q
  name                 = %[2]q
  description          = "Created by the Terraform provider acceptance tests"
  type                 = %[3]q
  allow_override_level = %[4]q
  default_value        = %[5]q
}
`, key, name, flagType, level, defaultValue)
}

func testAccCheckFeatureFlagGone(key string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ctx := context.Background()
		client, err := newManagementClient(ctx, kinde.NewClientOptions())
		if err != nil {
			return err
		}
		flags, err := (&FeatureFlagResource{client: client}).listFlags(ctx)
		if err != nil {
			return err
		}
		if _, ok := flags[key]; ok {
			return fmt.Errorf("feature flag %s still exists", key)
		}
		return nil
	}
}

type stubFlagDefinition struct {
	Name         string
	Description  string
	Type         string
	Level        string
	DefaultValue string
}

type stubFlagDefinitions struct {
	mu    sync.Mutex
	flags map[string]stubFlagDefinition
}

func (s *stubFlagDefinitions) get(key string) (stubFlagDefinition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flag, ok := s.flags[key]
	return flag, ok
}

func (s *stubFlagDefinitions) setDefault(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flag := s.flags[key]
	flag.DefaultValue = value
	s.flags[key] = flag
}

func (s *stubFlagDefinitions) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flags, key)
}

func writeStubErrors(w http.ResponseWriter, code, message string) {
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"code": code, "message": message}}})
}

func stubTypedValue(flagType, value string) any {
	switch flagType {
	case "bool":
		return value == "true"
	case "int":
		return json.Number(value)
	default:
		return value
	}
}

func (s *stubFlagDefinitions) handlers(keys ...string) map[string]http.HandlerFunc {
	handlers := map[string]http.HandlerFunc{
		"/api/v1/feature_flags": func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var body struct {
				Name               string `json:"name"`
				Description        string `json:"description"`
				Key                string `json:"key"`
				Type               string `json:"type"`
				AllowOverrideLevel string `json:"allow_override_level"`
				DefaultValue       string `json:"default_value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.AllowOverrideLevel == "" {
				writeStubErrors(w, "OVERRIDE_LEVEL_REQUIRED", "Override level is required")
				return
			}
			if _, exists := s.flags[body.Key]; exists {
				writeStubErrors(w, "KEY_NON_UNIQUE", "Key must be unique")
				return
			}
			s.flags[body.Key] = stubFlagDefinition{body.Name, body.Description, body.Type, body.AllowOverrideLevel, body.DefaultValue}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "FEATURE_FLAG_CREATED", "message": "Feature flag created"})
		},
		"/api/v1/environment/feature_flags": func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			flags := map[string]any{}
			for key, flag := range s.flags {
				flags[key] = map[string]any{"type": flag.Type, "value": stubTypedValue(flag.Type, flag.DefaultValue)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "OK", "feature_flags": flags})
		},
	}

	for _, key := range keys {
		handlers["/api/v1/feature_flags/"+key] = func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			flag, exists := s.flags[key]
			if !exists {
				writeStubErrors(w, "FEATURE_FLAG_NOT_FOUND", "Feature flag not found")
				return
			}
			switch r.Method {
			case http.MethodPut:
				query := r.URL.Query()
				if query.Get("key") == "" {
					writeStubErrors(w, "KEY_REQUIRED", "Key is required")
					return
				}
				flag.Name = query.Get("name")
				flag.Description = query.Get("description")
				flag.Type = query.Get("type")
				flag.Level = query.Get("allow_override_level")
				flag.DefaultValue = query.Get("default_value")
				s.flags[key] = flag
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "FEATURE_FLAG_UPDATED", "message": "Feature flag updated"})
			case http.MethodDelete:
				delete(s.flags, key)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "FEATURE_FLAG_DELETED", "message": "Feature flag deleted"})
			}
		}
	}

	return handlers
}

func TestFeatureFlagResourceAgainstStubServer(t *testing.T) {
	flags := &stubFlagDefinitions{flags: map[string]stubFlagDefinition{}}
	newStubManagementServer(t, flags.handlers("beta", "beta_v2"))

	expectFlag := func(key string, want stubFlagDefinition) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got, ok := flags.get(key)
			if !ok {
				return fmt.Errorf("expected flag %s to exist", key)
			}
			if got != want {
				return fmt.Errorf("flag %s = %+v, want %+v", key, got, want)
			}
			return nil
		}
	}
	expectNoFlag := func(key string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if _, ok := flags.get(key); ok {
				return fmt.Errorf("expected flag %s to be deleted", key)
			}
			return nil
		}
	}

	config := func(key, name, flagType, level, defaultValue string) string {
		return fmt.Sprintf(`
resource "kinde_feature_flag" "test" {
  key                  = %[1]q
  name                 = %[2]q
  description          = "Gates beta features"
  type                 = %[3]q
  allow_override_level = %[4]q
  default_value        = %[5]q
}
`, key, name, flagType, level, defaultValue)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			expectNoFlag("beta"),
			expectNoFlag("beta_v2"),
		),
		Steps: []resource.TestStep{
			{
				Config: config("beta", "Beta", "bool", "org", "false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "id", "beta"),
					expectFlag("beta", stubFlagDefinition{"Beta", "Gates beta features", "bool", "org", "false"}),
				),
			},
			{
				Config: config("beta", "Beta v2", "int", "usr", "5"),
				Check:  expectFlag("beta", stubFlagDefinition{"Beta v2", "Gates beta features", "int", "usr", "5"}),
			},
			{
				PreConfig:          func() { flags.setDefault("beta", "9") },
				Config:             config("beta", "Beta v2", "int", "usr", "5"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config("beta", "Beta v2", "int", "usr", "5"),
				Check:  expectFlag("beta", stubFlagDefinition{"Beta v2", "Gates beta features", "int", "usr", "5"}),
			},
			{
				ResourceName:            "kinde_feature_flag.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"name", "description", "allow_override_level"},
			},
			{
				PreConfig:          func() { flags.remove("beta") },
				Config:             config("beta", "Beta v2", "int", "usr", "5"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config("beta_v2", "Beta v2", "int", "usr", "5"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_feature_flag.test", "id", "beta_v2"),
					expectFlag("beta_v2", stubFlagDefinition{"Beta v2", "Gates beta features", "int", "usr", "5"}),
				),
			},
		},
	})
}

func TestFeatureFlagResourceValidatesConfig(t *testing.T) {
	cases := map[string]string{
		`type must be one of`:                 `type = "float"` + "\n" + `allow_override_level = "env"` + "\n" + `default_value = "1"`,
		`allow_override_level must be one of`: `type = "str"` + "\n" + `allow_override_level = "team"` + "\n" + `default_value = "x"`,
		`must be "true" or "false"`:           `type = "bool"` + "\n" + `allow_override_level = "env"` + "\n" + `default_value = "True"`,
		`must be a whole number`:              `type = "int"` + "\n" + `allow_override_level = "env"` + "\n" + `default_value = "1.5"`,
	}

	for want, attrs := range cases {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "kinde_feature_flag" "test" {
  key  = "beta"
  name = "Beta"
  %s
}
`, attrs),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(want)),
				},
			},
		})
	}
}
