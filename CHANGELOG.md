# Changelog

All notable changes to this provider are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial provider surface for the Kinde Management API.
  - Resources: `kinde_api`, `kinde_application`, `kinde_application_connection`,
    `kinde_connection`, `kinde_organization`, `kinde_organization_user`,
    `kinde_permission`, `kinde_role`, `kinde_user`, `kinde_user_role`.
  - Data sources: `kinde_api`, `kinde_application`, `kinde_connections`.
- `kinde_environment` data source and `kinde_environment_feature_flags`
  resource, built on the official
  [`kinde-oss/kinde-go`](https://github.com/kinde-oss/kinde-go) SDK.
- `kinde_feature_flag` resource for feature flag definitions.
- `kinde_organization_feature_flags` resource for organization feature flag overrides.
- `kinde_user_feature_flags` resource for user feature flag overrides. Kinde has
  no API to remove a user override, so removing one only stops Terraform
  managing it.

### Changed

- The provider now requires Go 1.24 to build, for the official Kinde Go SDK.

[Unreleased]: https://github.com/kinde-oss/terraform-provider-kinde/commits/main
