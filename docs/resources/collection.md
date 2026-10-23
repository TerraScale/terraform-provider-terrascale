---
page_title: "terrascale_collection Resource - TerraScale"
description: "Manage an empty schemaless TerraScale collection."
---

# terrascale_collection (Resource)

Requires a provider `project_token`: a public ts-auth project API key or project
access token authorized for the project and collection operations: `documents:read` for reads, and `documents:write` for
creation/deletion. Use both scopes for the complete lifecycle, including the
read-before-delete identity check. Management's user access token alone is
insufficient.

Public keys are exchanged for a cached token requesting only those two document
scopes. The server binds the token to the project in the key metadata; the
provider does not infer a project from the key ID. Static project JWTs remain
supported.

Deletion and replacement remove every document in the collection. The example
below assumes an existing `main` database. The default provider creates the key;
a separate alias uses it for collections without a provider dependency cycle.

```terraform
# The default provider uses TERRASCALE_ACCESS_TOKEN for key management.
provider "terrascale" {}

resource "terrascale_api_key" "collections" {
  organization_id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
  project_id      = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
  name            = "Terraform collections"
  database_name   = "main"
  scopes          = ["documents:read", "documents:write"]
}

# This alias depends on a key managed by the default provider, avoiding a cycle.
provider "terrascale" {
  alias         = "collections"
  project_token = terrascale_api_key.collections.secret
}

resource "terrascale_collection" "users" {
  provider      = terrascale.collections
  project_id    = terrascale_api_key.collections.project_id
  database_name = "main"
  name          = "users"

  lifecycle {
    prevent_destroy = true
  }
}
```

## Schema

- `project_id` (String, Required): canonical uppercase project ULID.
- `database_name` (String, Required): database name, `^[a-z][a-z0-9_-]{0,62}$`.
- `name` (String, Required): collection name with the same name format.
- `id` (String, Read-Only): collection ULID returned by the API.

Every configuration change requires replacement. Routes address names. Refresh
removes state if the name now belongs to a different collection ID. Before deletion,
the provider checks identity and refuses a changed ID. The API has no conditional
drop, so another actor can still recreate the name between that check and drop.
Avoid concurrent external lifecycle changes during apply/destroy.

## Import

Use `project_id/database_name/collection_name`. The project uses a plain ULID;
refresh obtains the collection's ULID by name:

```sh
terraform import terrascale_collection.users 01ARZ3NDEKTSV4RRFFQ69G5FAW/main/users
```
