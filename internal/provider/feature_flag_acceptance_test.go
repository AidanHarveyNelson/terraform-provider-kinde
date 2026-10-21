// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccManagementClient() (*ManagementAPI, error) {
	return newManagementClient(context.Background(), managementConfigFromEnv())
}

func testAccScope(r any) featureFlagOverrideScope {
	res, ok := r.(*featureFlagOverridesResource)
	if !ok {
		panic(fmt.Sprintf("unexpected resource type %T", r))
	}
	return res.scope
}

func testAccEnvironmentScope() featureFlagOverrideScope {
	return testAccScope(NewEnvironmentFeatureFlagsResource())
}

func testAccOrganizationScope() featureFlagOverrideScope {
	return testAccScope(NewOrganizationFeatureFlagsResource())
}

func testAccUserScope() featureFlagOverrideScope {
	return testAccScope(NewUserFeatureFlagsResource())
}

func testAccCheckKindeFlagValues(scope featureFlagOverrideScope, owner *string, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ctx := context.Background()
		client, err := testAccManagementClient()
		if err != nil {
			return err
		}

		got, err := listFeatureFlagValues(ctx, client, scope.listPath(*owner))
		if err != nil {
			return err
		}

		for _, key := range sortedKeys(want) {
			value, ok := got[key]
			if !ok {
				return fmt.Errorf("feature flag %s not found in Kinde", key)
			}
			if !sameFeatureFlagValue(value, want[key]) {
				return fmt.Errorf("feature flag %s in Kinde is %q, want %q", key, value, want[key])
			}
		}
		return nil
	}
}

func testAccSetKindeOverride(t *testing.T, scope featureFlagOverrideScope, owner *string, key, value string) {
	t.Helper()
	ctx := context.Background()

	client, err := testAccManagementClient()
	if err != nil {
		t.Fatalf("failed to create management client: %s", err)
	}
	if err := scope.setOverride(ctx, client, *owner, key, value); err != nil {
		t.Fatalf("failed to set override %s=%q in Kinde: %s", key, value, err)
	}
}

func testAccCaptureAttr(resourceName, attribute string, dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not found in state", resourceName)
		}
		value, ok := rs.Primary.Attributes[attribute]
		if !ok || value == "" {
			return fmt.Errorf("%s has no %s in state", resourceName, attribute)
		}
		*dst = value
		return nil
	}
}

func testAccCheckKindeFlagDefinition(key, wantType, wantValue string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ctx := context.Background()
		client, err := testAccManagementClient()
		if err != nil {
			return err
		}

		flags, err := (&FeatureFlagResource{client: client}).listFlags(ctx)
		if err != nil {
			return err
		}
		flag, ok := flags[key]
		if !ok {
			return fmt.Errorf("feature flag %s not found in Kinde", key)
		}
		if flag.Type != wantType {
			return fmt.Errorf("feature flag %s in Kinde has type %q, want %q", key, flag.Type, wantType)
		}
		if !sameFeatureFlagValue(flag.Value, wantValue) {
			return fmt.Errorf("feature flag %s in Kinde has value %q, want %q", key, flag.Value, wantValue)
		}
		return nil
	}
}

func testAccUpdateKindeFlagDefault(t *testing.T, key, name, flagType, level, defaultValue string) {
	t.Helper()

	client, err := testAccManagementClient()
	if err != nil {
		t.Fatalf("failed to create management client: %s", err)
	}

	query := url.Values{}
	query.Set("key", key)
	query.Set("name", name)
	query.Set("type", flagType)
	query.Set("allow_override_level", level)
	query.Set("default_value", defaultValue)
	if err := client.requestJSON(context.Background(), http.MethodPut, "/api/v1/feature_flags/"+url.PathEscape(key), query, nil); err != nil {
		t.Fatalf("failed to update feature flag %s in Kinde: %s", key, err)
	}
}
