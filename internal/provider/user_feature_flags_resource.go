// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

func NewUserFeatureFlagsResource() resource.Resource {
	return &featureFlagOverridesResource{scope: featureFlagOverrideScope{
		typeName: "_user_feature_flags",
		label:    "User",
		description: "Overrides feature flag values for a user. " +
			"Kinde only accepts user overrides for flags whose `allow_override_level` is `usr`. " +
			"**Kinde has no API to remove a user override:** removing a flag from `flags` or destroying this resource " +
			"only stops Terraform managing it, and the override keeps its last value in Kinde until it is changed there. " +
			"See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/users) for more details.",
		ownerAttribute:   "user_id",
		ownerDescription: "The ID of the user. Changing it replaces the resource.",
		removable:        false,
		listPath: func(userID string) string {
			return "/api/v1/users/" + url.PathEscape(userID) + "/feature_flags"
		},
		setOverride: func(ctx context.Context, client *ManagementAPI, userID, key, value string) error {
			res, err := client.UpdateUserFeatureFlagOverride(ctx, management_api.UpdateUserFeatureFlagOverrideParams{
				UserID:         userID,
				FeatureFlagKey: key,
				Value:          value,
			})
			if err != nil {
				return fmt.Errorf("could not set override for feature flag %q: %w", key, err)
			}
			if _, ok := res.(*management_api.SuccessResponse); !ok {
				return managementAPIError(fmt.Sprintf("set override for feature flag %q", key), res)
			}
			return nil
		},
	}}
}
