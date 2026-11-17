# Terraform Provider for TerraScale

Manage projects, databases and project API keys with the
[Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework)
and the public [`terrascale.tech/sdk`](https://pkg.go.dev/terrascale.tech/sdk), at the version `go.mod` requires.
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

- `TERRASCALE_ACCESS_TOKEN`: ts-cli user access token from ts-auth, used for project,
  database and API-key management. The provider exchanges it for a cached project
  token with `databases:read` and `databases:write` for database operations.
- `TERRASCALE_PROJECT_TOKEN`: public ts-auth project API key or project access token
  for project operations. Public keys are exchanged for cached tokens with
  `databases:read` and `databases:write`; project JWTs are used directly.

Explicit provider attributes override environment defaults. Keep credentials out
of checked-in HCL and shell arguments. Never use internal peer credentials.
API-key secrets are stored in Terraform state. Sensitive marking hides display;
use an encrypted backend with restricted access. Imports cannot recover secrets.

## Resource behavior

| Resource | In-place updates | Import identifier |
|---|---|---|
| `terrascale_project` | Display name | `organization_id/project_id` |
| `terrascale_database` | Serving regions and all-region placement | `organization_id/project_id/database_id` |
| `terrascale_api_key` | None; changes replace and revoke | `organization_id/project_id/key_id` |

Entity IDs are canonical uppercase ULIDs. Database names match
`^[a-z][a-z0-9_-]{0,62}$`. Projects and keys accept display names of 1–128 UTF-16
code units, including a letter or digit, without controls or surrounding whitespace.
Database replacement/deletion destroys data; use `lifecycle.prevent_destroy` where
appropriate. Reads, updates and deletion address the immutable database ULID.

The current API selects complete serving `regions` or `all_regions`; it has no
home/replica split. `home_region` and `replica_regions` have been removed. Use
`regions` and re-import existing databases to populate the current schema.
The retired `terrascale_collection` resource and document scopes have been removed.
API keys accept `databases:read`, `databases:write`, `sql:read` and `sql:write`.

Refresh removes resources only on typed not-found responses. Other failures,
including `unknown_route`, preserve state and fail. Error diagnostics include
status, validated code, guidance and request ID without token values.

## Development and release preparation

```sh
nice -n 19 go build ./...
```

CI (`ci.yml`, required check `all-green`) runs the legacy-term scan, `go vet ./...`, `go build ./...`, govulncheck and `goreleaser check`; the tag release also checks the six unsigned archives and the protocol manifest before signing.
The daily `Advisories` workflow runs the same govulncheck scans on `main`; when it fails, it opens the `Advisory scan failed` issue or comments on the open one.

[Release preparation](docs/release.md) documents the unsigned dry run and the
user-only GPG/Registry setup. No production apply/destroy was performed.
