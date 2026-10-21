# Kinde Terraform Provider

The Kinde provider for Terraform.

> **📚 Management API**: This provider is built on the Kinde Management API. For information about authentication, scopes, and the underlying endpoints, see the [Management API docs](https://docs.kinde.com/kinde-apis/management/).

[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square)](https://makeapullrequest.com) [![Kinde Docs](https://img.shields.io/badge/Kinde-Docs-eee?style=flat-square)](https://kinde.com/docs/developer-tools) [![Kinde Community](https://img.shields.io/badge/Kinde-Community-eee?style=flat-square)](https://thekindecommunity.slack.com)

Manage your Kinde business as code — applications, APIs, connections, organizations, users, roles, and permissions.

## Development

Requires Terraform 1.0+ and Go 1.24+

### Running the tests

| Command | What it runs |
| --- | --- |
| `make test` | Unit tests. No Kinde account needed; resources run end to end against a stub Management API. |
| `make testacc` | All acceptance tests against a real Kinde business. |
| `make test-feature-flags` | Unit tests for the environment and feature flag resources. |
| `make testacc-feature-flags` | Acceptance tests for the environment and feature flag resources. |

Acceptance tests read credentials from a `.env` file in the repository root (copy `.env.example`). Use a non-production environment: the tests create and delete real objects, all prefixed `tfacc`. The M2M application needs the scopes listed under [Prerequisites](#prerequisites) for the resources under test, plus `create:feature_flags`, `update:feature_flags` and `delete:feature_flags` for the feature flag tests.

The feature flag acceptance tests check values in Kinde directly, not just in Terraform state, including drift made outside Terraform and that destroy leaves unmanaged overrides alone.

## Getting started

### Prerequisites

The provider authenticates as a Machine-to-Machine (M2M) application. Before you begin, create an M2M application in Kinde, authorize it for the Management API, and grant the scopes for the resources you intend to manage.

Management API scopes are resource-specific. Grant only the scopes for the resources in your configuration:

| Resource | Required scopes |
| --- | --- |
| `kinde_api` | `create:apis`, `read:apis`, `delete:apis` |
| `kinde_application` | `create:applications`, `read:applications`, `update:applications`, `delete:applications` |
| `kinde_application_connection` | `create:application_connections`, `read:application_connections`, `delete:application_connections` |
| `kinde_connection` | `create:connections`, `read:connections`, `update:connections`, `delete:connections` |
| `kinde_environment_feature_flags` | `read:environment_feature_flags`, `update:environment_feature_flags`, `delete:environment_feature_flags` |
| `kinde_feature_flag` | `create:feature_flags`, `update:feature_flags`, `delete:feature_flags`, `read:environment_feature_flags` |
| `kinde_organization` | `create:organizations`, `read:organizations`, `update:organizations`, `delete:organizations` |
| `kinde_organization_feature_flags` | `read:organization_feature_flags`, `update:organization_feature_flags`, `delete:organization_feature_flags` |
| `kinde_organization_user` | `create:organization_users`, `read:organization_users`, `update:organization_users`, `delete:organization_users` |
| `kinde_permission` | `create:permissions`, `read:permissions`, `update:permissions`, `delete:permissions` |
| `kinde_role` | `create:roles`, `read:roles`, `update:roles`, `delete:roles`, `read:role_permissions`, `update:role_permissions`, `delete:role_permissions` |
| `kinde_user` | `create:users`, `read:users`, `update:users`, `delete:users`, `create:user_identities`, `read:user_identities` |
| `kinde_user_feature_flags` | `update:user_feature_flags` |
| `kinde_user_role` | `create:organization_user_roles`, `read:organization_user_roles`, `delete:organization_user_roles` |

| Data source | Required scopes |
| --- | --- |
| `kinde_api` | `read:apis` |
| `kinde_application` | `read:applications` |
| `kinde_connections` | `read:connections` |
| `kinde_environment` | `read:environments` |

Setting the `roles` attribute on `kinde_organization_user` also requires the `organization_user_roles` scopes listed above for `kinde_user_role`.

`kinde_api` has no update scope because the Kinde API does not support updating an API — changing one replaces it.

Grant the `delete:` scopes for anything you manage. Without them Terraform cannot destroy what it creates, and a failed apply may leave resources behind that need cleaning up by hand.

### Installation

Add the provider to your configuration:

```terraform
terraform {
  required_providers {
    kinde = {
      source  = "kinde-oss/kinde"
      version = "~> 0.1"
    }
  }
}
```

Then initialize:

```bash
terraform init
```

### Authentication

Configure the provider with your M2M credentials:

```terraform
provider "kinde" {
  domain        = "https://example.kinde.com"
  audience      = "https://example.kinde.com/api"
  client_id     = "your_client_id"
  client_secret = "your_client_secret"
}
```

Every attribute can also be set through the environment, which keeps credentials out of your configuration:

| Attribute | Environment variable |
| --- | --- |
| `domain` | `KINDE_DOMAIN` |
| `audience` | `KINDE_AUDIENCE` |
| `client_id` | `KINDE_CLIENT_ID` |
| `client_secret` | `KINDE_CLIENT_SECRET` |

With those set, the provider block can be left empty:

```terraform
provider "kinde" {}
```

## Resources

| Resource | Description |
| --- | --- |
| `kinde_api` | APIs registered with your business |
| `kinde_application` | Applications (regular, SPA, or M2M) |
| `kinde_application_connection` | Enables a connection on an application |
| `kinde_connection` | Social and enterprise identity connections |
| `kinde_environment_feature_flags` | Feature flag overrides for the environment |
| `kinde_feature_flag` | Feature flag definitions |
| `kinde_organization` | Organizations, including theme colors and handle |
| `kinde_organization_feature_flags` | Feature flag overrides for an organization |
| `kinde_organization_user` | Membership of a user in an organization |
| `kinde_permission` | Permissions |
| `kinde_role` | Roles and their assigned permissions |
| `kinde_user` | Users and their identities |
| `kinde_user_feature_flags` | Feature flag overrides for a user (Kinde can't remove user overrides) |
| `kinde_user_role` | Assignment of a role to a user in an organization |

## Data sources

| Data source | Description |
| --- | --- |
| `kinde_api` | Look up an existing API |
| `kinde_application` | Look up an existing application |
| `kinde_connections` | List connections in your business |
| `kinde_environment` | Look up the environment the provider is authenticated against |

### Key Features

- Lifecycle support varies by resource; see the resource documentation for supported operations
- Import support for adopting existing Kinde configuration
- Paginated reads, so large roles and permission sets are not silently truncated
- Automatic rollback of partially created resources when a create fails

Schema documentation for every resource and data source is in [docs/](docs/).

## Usage

Create a permission and attach it to a role:

```terraform
resource "kinde_permission" "read_billing" {
  name        = "Read billing"
  key         = "read:billing"
  description = "Grants read access to billing"
}

resource "kinde_role" "finance" {
  name        = "Finance"
  key         = "finance"
  description = "Finance team role"
  permissions = [kinde_permission.read_billing.id]
}
```

Create an application:

```terraform
resource "kinde_application" "web" {
  name          = "Web app"
  type          = "reg"
  login_uri     = "https://example.com/oauth/login"
  homepage_uri  = "https://example.com"
  logout_uris   = ["https://example.com/oauth/logout"]
  redirect_uris = ["https://example.com/oauth/callback"]
}
```

Add a user to an organization and grant them a role:

```terraform
resource "kinde_organization" "acme" {
  name = "Acme"
}

resource "kinde_user" "jane" {
  first_name = "Jane"
  last_name  = "Doe"

  identities = [
    {
      type  = "email"
      value = "jane@example.com"
    }
  ]
}

resource "kinde_organization_user" "jane_acme" {
  organization_code = kinde_organization.acme.code
  user_id           = kinde_user.jane.id

  # kinde_user_role manages role assignments for this membership.
  lifecycle {
    ignore_changes = [roles]
  }
}

resource "kinde_user_role" "jane_finance" {
  organization_code = kinde_organization.acme.code
  user_id           = kinde_user.jane.id
  role_id           = kinde_role.finance.id

  # A user must be a member of the organization before roles can be assigned.
  depends_on = [kinde_organization_user.jane_acme]
}
```

A role can only be assigned to a user who is already a member of the organization. Because `kinde_user_role` references the organization and user directly rather than the membership, Terraform cannot infer that ordering — declare it with `depends_on`.

## Importing existing resources

Every resource supports import, so existing Kinde configuration can be brought under Terraform management:

```bash
terraform import kinde_role.finance role_01H8XYZ
```

Resources that exist in the context of another resource take a composite ID, separated by colons:

| Resource | Import ID |
| --- | --- |
| `kinde_application_connection` | `application_id:connection_id` |
| `kinde_organization_user` | `organization_code:user_id` |
| `kinde_user_role` | `organization_code:user_id:role_id` |

## Examples

Runnable example modules for each resource are in [examples/](examples/).

Terraform resolves providers from the registry, so testing a local build requires a development override. To run the full lifecycle — `init`, `validate`, `plan`, `apply`, a second `plan` to check for drift, then `destroy` — against an example:

```bash
make example-smoke EXAMPLE=examples/resources/kinde_role
```

Use `make example-smoke-all` to run it across every resource example. See [examples/README.md](examples/README.md) for details.

### Provider development

1. Clone the repository to your machine:

   ```bash
   git clone https://github.com/kinde-oss/terraform-provider-kinde.git
   ```

2. Go into the project:

   ```bash
   cd terraform-provider-kinde
   ```

3. Install the dependencies:

   ```bash
   go mod download
   ```

4. Install the tools the `make` targets call. `make build` runs `docs-validate`, which needs `tfplugindocs`, and `make lint` needs `golangci-lint`. Use the versions this repository pins, so local runs match CI:

   ```bash
   go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.19.4
   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
   ```

5. Build the provider and run the unit tests:

   ```bash
   make build
   make test
   ```

Acceptance tests create and destroy real resources in your Kinde business, and may cost money to run. Copy `.env.example` to `.env`, add your M2M credentials, then:

```bash
make testacc
```

Documentation in `docs/` is generated from the resource schemas and the files in `examples/`. Regenerate it after any schema change, or CI will fail:

```bash
cd tools && go generate ./...
```

This runs the generators through the nested `tools` module, so it works without the installed binaries from step 4.

Other useful targets:

| Target | Description |
| --- | --- |
| `make fmt` | Format Go code |
| `make lint` | Run golangci-lint |
| `make docs-validate` | Validate generated documentation |
| `make coverage` | Generate and open a coverage report |
| `make all` | Run all pre-commit checks |

## Documentation

For details on using this provider in your project, head over to the [Kinde docs](https://kinde.com/docs/) and see the [developer tools](https://kinde.com/docs/developer-tools/) docs 👍🏼.

## Publishing

The core team handles publishing.

## Contributing

Please refer to Kinde’s [contributing guidelines](https://github.com/kinde-oss/.github/blob/main/.github/CONTRIBUTING.md).

## License

By contributing to Kinde, you agree that your contributions will be licensed under its Mozilla Public License 2.0. See [LICENSE](LICENSE) for details.
