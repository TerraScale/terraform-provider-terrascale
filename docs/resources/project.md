---
page_title: "terrascale_project Resource - TerraScale"
description: "Manage a TerraScale project within an existing organization."
---

# terrascale_project (Resource)

Requires the provider's user `access_token`. A project must be empty before deletion.

```terraform
resource "terrascale_project" "app" {
  organization_id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
  name            = "Application"
}
```

## Schema

- `organization_id` (String, Required): canonical uppercase organization ULID.
  Changes replace the project.
- `name` (String, Required): display name, 1–128 UTF-16 code units, containing a
  letter or digit, no controls or surrounding whitespace. Updates rename in place.
- `id` (String, Read-Only): project ULID.

## Import

Use `organization_id/project_id`, both plain ULIDs:

```sh
terraform import terrascale_project.app 01ARZ3NDEKTSV4RRFFQ69G5FAV/01ARZ3NDEKTSV4RRFFQ69G5FAW
```

Refresh removes state on HTTP 404 `not_found`, including membership loss hidden by the API.
Verify access before applying a recreation plan.
