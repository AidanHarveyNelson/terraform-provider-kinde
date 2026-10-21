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

func NewOrganizationFeatureFlagsResource() resource.Resource {
	return &featureFlagOverridesResource{scope: featureFlagOverrideScope{
		typeName: "_organization_feature_flags",
		label:    "Organization",
		description: "Overrides feature flag values for an organization. " +
			"Only the flags listed in `flags` are managed; overrides set outside Terraform on other flags are left alone. " +
			"Kinde only accepts organization overrides for flags whose `allow_override_level` is `org`. " +
			"See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/organizations) for more details.",
		ownerAttribute:   "org_code",
		ownerDescription: "The code of the organization. Changing it replaces the resource.",
		removable:        true,
		listPath: func(orgCode string) string {
			return "/api/v1/organizations/" + url.PathEscape(orgCode) + "/feature_flags"
		},
		setOverride: func(ctx context.Context, client *ManagementAPI, orgCode, key, value string) error {
			res, err := client.UpdateOrganizationFeatureFlagOverride(ctx, management_api.UpdateOrganizationFeatureFlagOverrideParams{
				OrgCode:        orgCode,
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
		deleteOverride: func(ctx context.Context, client *ManagementAPI, orgCode, key string) error {
			res, err := client.DeleteOrganizationFeatureFlagOverride(ctx, management_api.DeleteOrganizationFeatureFlagOverrideParams{
				OrgCode:        orgCode,
				FeatureFlagKey: key,
			})
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
