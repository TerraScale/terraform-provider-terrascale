---
page_title: "terrascale_database Resource - TerraScale"
description: "Manage a TerraScale database and its replica placement."
---

# terrascale_database (Resource)

Requires the provider's user `access_token`. Replacement and deletion remove data.

```terraform
resource "terrascale_database" "main" {
  organization_id = terrascale_project.app.organization_id
  project_id      = terrascale_project.app.id
  name            = "main"
  home_region     = "gru-1"
  replica_regions = ["fra-1"]

  lifecycle {
    prevent_destroy = true
  }
}
```

## Schema

- `organization_id`, `project_id` (String, Required): canonical uppercase parent
  ULIDs. Parent changes replace the database.
- `name` (String, Required): `^[a-z][a-z0-9_-]{0,62}$`. Changes replace the database.
- `home_region` (String, Optional/Computed): region ID such as `gru-1`, selected by
  the API when omitted. Changes replace the database.
- `replica_regions` (Set of String, Optional/Computed): complete replica region
  IDs. When both placement controls are omitted, creation accepts the API's
  selected other serving regions and existing/imported placement is retained.
  Explicit lists update in place and select listed mode when `all_regions` is
  omitted.
  Explicit empty creation is rejected before mutation unless `all_regions=true`.
  Empty is supported for updates, for example before deletion, but can suspend
  writes until a replica is added.
- `all_regions` (Boolean, Optional/Computed): replicate to every serving region,
  including future regions. Omitting both placement controls accepts API behavior
  on creation and preserves existing/imported placement. Setting only
  `replica_regions` selects listed mode (`all_regions=false`). True requires empty
  or omitted `replica_regions`. Updates in place; explicitly setting false uses the configured
  replica set, or the existing replica set when omitted.
- `id` (String, Read-Only): database ULID.

The provider preserves backend errors, including refusal to delete replicated
databases. It never automatically removes replicas to make destruction succeed.

## Import

Use `organization_id/project_id/database_id`, all plain ULIDs:

```sh
terraform import terrascale_database.main 01ARZ3NDEKTSV4RRFFQ69G5FAV/01ARZ3NDEKTSV4RRFFQ69G5FAW/01ARZ3NDEKTSV4RRFFQ69G5FAX
```

Omitted placement preserves the imported home region, replica list and all-region
setting. Explicit placement configuration remains managed and must match the
intended live placement before applying.
