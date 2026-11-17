---
page_title: "terrascale_database Resource - TerraScale"
description: "Manage a TerraScale database and its serving regions."
---

# terrascale_database (Resource)

Requires the provider's ts-cli user `access_token`. The provider exchanges it for
a project token with `databases:read` and `databases:write`. Replacement and
deletion remove data. Reads, placement updates and deletion address the immutable
database ULID, so a different database recreated at the old name is never adopted.

```terraform
resource "terrascale_database" "main" {
  organization_id = terrascale_project.app.organization_id
  project_id      = terrascale_project.app.id
  name            = "main"
  regions         = ["gru", "fra"]

  lifecycle {
    prevent_destroy = true
  }
}
```

## Schema

- `organization_id`, `project_id` (String, Required): canonical uppercase parent
  ULIDs. Parent changes replace the database.
- `name` (String, Required): `^[a-z][a-z0-9_-]{0,62}$`. Changes replace the database.
- `regions` (Set of String, Optional/Computed): complete set of one to nine serving
  region IDs. Omit both placement controls to accept server placement on creation
  and preserve existing/imported placement. Explicit regions select listed mode
  when `all_regions` is omitted. Empty sets are rejected.
- `all_regions` (Boolean, Optional/Computed): serve from every region, including
  future regions. Cannot be combined with explicitly configured `regions`.
  Setting false retains the current serving regions unless another set is
  configured. Omit both controls to preserve existing placement.
- `id` (String, Read-Only): database ULID.

The current API uses one set of serving regions. The earlier `home_region` and
`replica_regions` attributes have been removed. Replace them in configuration with
`regions` containing every desired serving region; re-import existing databases
to populate state with the current schema before planning.

## Import

Use `organization_id/project_id/database_id`, all plain ULIDs:

```sh
terraform import terrascale_database.main 01ARZ3NDEKTSV4RRFFQ69G5FAV/01ARZ3NDEKTSV4RRFFQ69G5FAW/01ARZ3NDEKTSV4RRFFQ69G5FAX
```

Import reads the database directly by its immutable ULID. Omitted placement
preserves the imported regions and all-region setting. Explicit placement must
match the intended live placement before applying.
