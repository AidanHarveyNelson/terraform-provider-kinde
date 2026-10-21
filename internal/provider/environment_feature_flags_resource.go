// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

func NewEnvironmentFeatureFlagsResource() resource.Resource {
	return &featureFlagOverridesResource{scope: featureFlagOverrideScope{
		typeName: "_environment_feature_flags",
		label:    "Environment",
		description: "Overrides feature flag values for the environment the provider is authenticated against. " +
			"Only the flags listed in `flags` are managed; overrides set outside Terraform on other flags are left alone. " +
			"Declare this resource once per environment. " +
			"See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/environments) for more details.",
		fixedID:   "environment",
		removable: true,
		listPath: func(string) string {
			return "/api/v1/environment/feature_flags"
		},
		setOverride: func(ctx context.Context, client *ManagementAPI, _, key, value string) error {
			res, err := client.UpdateEnvironementFeatureFlagOverride(ctx,
				&management_api.UpdateEnvironementFeatureFlagOverrideReq{Value: value},
				management_api.UpdateEnvironementFeatureFlagOverrideParams{FeatureFlagKey: key},
			)
			if err != nil {
				return fmt.Errorf("could not set override for feature flag %q: %w", key, err)
			}
			if _, ok := res.(*management_api.SuccessResponse); !ok {
				return managementAPIError(fmt.Sprintf("set override for feature flag %q", key), res)
			}
			return nil
		},
		deleteOverride: func(ctx context.Context, client *ManagementAPI, _, key string) error {
			res, err := client.DeleteEnvironementFeatureFlagOverride(ctx,
				management_api.DeleteEnvironementFeatureFlagOverrideParams{FeatureFlagKey: key},
			)
			if err != nil {
				return fmt.Errorf("could not delete override for feature flag %q: %w", key, err)
			}
			if _, ok := res.(*management_api.SuccessResponse); !ok {
				return managementAPIError(fmt.Sprintf("delete override for feature flag %q", key), res)
			}
			return nil
		},
	}}
}
