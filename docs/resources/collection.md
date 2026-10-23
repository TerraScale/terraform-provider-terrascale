---
page_title: "terrascale_collection Resource - TerraScale"
description: "Manage an empty schemaless TerraScale collection."
---

# terrascale_collection (Resource)

Requires an existing provider `project_token` authorized for the project and
collection operations: `documents:read` for reads, and `documents:write` for
creation/deletion. Use both scopes for the complete lifecycle, including the
read-before-delete identity check. Management's user access token alone is
insufficient.
Deletion and replacement remove every document in the collection.

```terraform
resource "terrascale_collection" "users" {
  project_id    = terrascale_database.main.project_id
  database_name = terrascale_database.main.name
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
