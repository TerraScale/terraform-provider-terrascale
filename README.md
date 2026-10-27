# Terraform Provider for TerraScale

Manage projects, databases, collections and project API keys with the
[Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework)
and the public [`terrascale.tech/sdk v0.0.6`](https://pkg.go.dev/terrascale.tech/sdk@v0.0.6).
Every SDK request is bounded: 10 s dial and TLS handshake, 30 s response
headers and a 30 s deadline per operation including retries, so a stalled
connection fails the apply instead of hanging it.
Projects and API keys use `https://auth.terrascale.tech/v1/terrascale`.
Databases use the public `https://api.terrascale.tech/v1/projects` API with a
cached project token exchanged from the existing ts-cli user access token.
The exchange requests only `databases:read` and `databases:write`.

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

- `TERRASCALE_ACCESS_TOKEN`: existing **ts-cli user access token from ts-auth**, used by
  project, database and API-key management. An organization ID is not a credential.
  Never use an internal peer credential.
- `TERRASCALE_PROJECT_TOKEN`: **public ts-auth project API key or project access token**,
  needed only for collections. It must authorize the collection's project and
  collection operations. Collection reads require `documents:read`; creation and
  deletion require `documents:write`. Use both scopes for the provider's complete
  collection lifecycle, including its read-before-delete identity check. Public
  keys (`<ULID>_<52 base32 characters>`) are exchanged for a cached token with
  only those two scopes; ts-auth derives the project from the key metadata.
  Project access tokens are sent directly.

Explicit `access_token` and `project_token` provider attributes override the
corresponding environment value. Keep credentials out of checked-in HCL and shell
arguments. Use provider aliases with separate existing credentials for collections
in different projects. Do not configure a provider using a resource managed by
that same provider, which introduces a dependency cycle. A key created by the
default management provider can configure a separate collection provider alias;
see the [collection example](docs/resources/collection.md).

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

Database and collection routes address names. Refresh never adopts a different
ID at the same name; database placement updates and both resources' deletions
verify identity first. The API has no conditional delete: concurrent
deletion/recreation between the check and mutation cannot be prevented. Avoid
concurrent external lifecycle changes during apply/destroy.

Refresh removes resources only on a typed not-found 404 (`unknown_database`,
`unknown_collection` or ts-auth `not_found`) and removes revoked keys absent
from a full paginated key list. Other errors, including a 404 `unknown_route`
or an untyped proxy 404, preserve state and fail. Error diagnostics name the
HTTP status, the validated error code with guidance, Retry-After and the
request ID; server messages and credentials are omitted. A data-plane 404
`unknown_database` also answers a `project_token` for a different project. A management 404 can
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
They cover public auth/API routes, token exchange, CRUD, pagination, 404 removal,
permission and expired-token errors, cancellation, imports,
secret preservation, schema validation and replacement planning. GitHub CI's
`test` check also validates six unsigned release archives and the protocol manifest.
CI is the gate; a local test run is not required before pushing.

[Release preparation](docs/release.md) documents the unsigned dry run and the
user-only GPG/Registry setup. No production apply/destroy was performed.
