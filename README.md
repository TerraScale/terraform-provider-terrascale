# Terraform Provider for TerraScale

Manage projects, databases, collections and project API keys with the
[Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework)
and the public [`terrascale.tech/sdk v0.0.1`](https://pkg.go.dev/terrascale.tech/sdk@v0.0.1).
The provider always uses `https://api.terrascale.tech`.

This source prepares the initial **0.0.1** release. Registry
publication still requires user setup; the Registry installation below becomes
available after that release is published.

```hcl
terraform {
  required_providers {
    terrascale = {
      source  = "terrascale/terrascale"
      version = "0.0.1"
    }
  }
}

# Credentials come from TERRASCALE_ACCESS_TOKEN and TERRASCALE_PROJECT_TOKEN.
provider "terrascale" {}

resource "terrascale_project" "app" {
  organization_id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
  name            = "Application"
}

resource "terrascale_database" "main" {
  organization_id = terrascale_project.app.organization_id
  project_id      = terrascale_project.app.id
  name            = "main"

  lifecycle {
    prevent_destroy = true
  }
}
```

## Credentials and state

- `TERRASCALE_ACCESS_TOKEN`: existing **ts-auth user access token**, used by
  project, database and API-key management. An organization ID is not a credential.
  Never use an internal peer credential.
- `TERRASCALE_PROJECT_TOKEN`: existing **project API key or project access token**,
  needed only for collections. It must authorize the collection's project and
  collection operations. Collection reads require `documents:read`; creation and
  deletion require `documents:write`. Use both scopes for the provider's complete
  collection lifecycle, including its read-before-delete identity check.

Explicit `access_token` and `project_token` provider attributes override the
corresponding environment value. Keep credentials out of checked-in HCL and shell
arguments. Use provider aliases with separate existing credentials for collections
in different projects. Do not configure a provider using a resource managed by
that same provider, which introduces a dependency cycle.

`terrascale_api_key.secret` is returned once and **stored in Terraform state**.
Sensitive marking hides normal display; it does not encrypt state or saved plans.
Use an encrypted state backend with restricted access. Imported keys have a null
secret and are not rotated automatically. See [API key documentation](docs/resources/api_key.md).

## Resource behavior

| Resource | In-place updates | Import identifier |
|---|---|---|
| `terrascale_project` | Display name | `organization_id/project_id` |
| `terrascale_database` | Replica regions and all-region placement | `organization_id/project_id/database_id` |
| `terrascale_collection` | None; changes replace | `project_id/database_name/collection_name` |
| `terrascale_api_key` | None; changes replace and revoke | `organization_id/project_id/key_id` |

Every entity ID is a canonical uppercase ULID without prefixes. Database and
collection names match `^[a-z][a-z0-9_-]{0,62}$`. Projects and keys accept display
names of 1–128 UTF-16 code units, including a letter or digit, without control
characters or surrounding whitespace.

Database and collection replacement/deletion destroys data. Use
`lifecycle.prevent_destroy` where appropriate. Database replicas must be explicitly
cleared before deletion; the provider does not clear them automatically. A project
must contain no databases before deletion.

Collection routes address names. The provider checks the collection ID before
deletion and refuses a different identity, but the API has no conditional drop:
concurrent deletion/recreation between that check and drop cannot be prevented.
Avoid concurrent external collection lifecycle changes during apply/destroy.

Refresh removes resources on HTTP 404 and removes revoked keys absent from a full
paginated key list. Other errors preserve state and fail. A management 404 can
also mean that the user lost organization membership; verify access before
applying a resulting recreation plan. Import configuration must match the live
resource before applying. Omitted placement is computed: new databases accept the
server's chosen replicas, and refresh/import retain existing placement. Explicit
replica settings are managed as configured. An explicit empty replica list is
valid for updates (which can suspend writes), but creation requires a nonempty
list, omitted placement, or `all_regions=true`. Uncertain mutations are
not replayed by the SDK: inspect or import before retrying a failed creation.

## Development and release preparation

```sh
nice -n 19 go test ./...
nice -n 19 go build ./...
```

Tests use `httptest` HTTP fakes only: no TerraBase binary or production credentials.
They cover CRUD, pagination, 404 removal, permission errors, cancellation, imports,
secret preservation, schema validation and replacement planning. GitHub CI's
`test` check also validates six unsigned release archives and the protocol manifest.
CI is the gate; a local test run is not required before pushing.

[Release preparation](docs/release.md) documents the unsigned dry run and the
user-only GPG/Registry setup. No production apply/destroy was performed.
