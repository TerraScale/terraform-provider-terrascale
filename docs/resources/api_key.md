---
page_title: "terrascale_api_key Resource - TerraScale"
description: "Manage a TerraScale project API key with a sensitive one-time secret."
---

# terrascale_api_key (Resource)

Requires the provider's user `access_token`. Creating this resource issues a real
credential; no key is created by merely building or testing the provider.

**The secret is stored in Terraform state.** Sensitive marking only hides display.
Encrypt and restrict access to state and saved plans. The backend returns the
secret only once; imports and later refreshes cannot recover it.

```terraform
resource "terrascale_api_key" "reader" {
  organization_id = terrascale_project.app.organization_id
  project_id      = terrascale_project.app.id
  name            = "Document reader"
  scopes          = ["databases:read"]
  database_id     = terrascale_database.main.id
}

output "reader_secret" {
  value     = terrascale_api_key.reader.secret
  sensitive = true
}
```

## Schema

- `organization_id`, `project_id` (String, Required): canonical uppercase parent
  ULIDs.
- `name` (String, Required): display name, 1–128 UTF-16 code units, including a
  letter or digit, no controls or surrounding whitespace.
- `scopes` (Set of String, Required): nonempty subset of `databases:read`,
  `databases:write`, `sql:read` and `sql:write`.
- `database_id` (String, Optional): canonical uppercase database ULID, limiting
  the key to that immutable database. Omit to reach all databases in the project.
- `id` (String, Read-Only): API key ULID.
- `secret` (String, Sensitive, Read-Only): one-time secret retained across refresh.
  Null for imports.

Every configuration change replaces the key and revokes the old key. Deletion
revokes immediately. `databases:read` permits database metadata and rules reads;
`databases:write` permits database management and rules deployment. SQL access
uses `sql:read` and `sql:write`. Document scopes have been removed.
A key with database read/write scopes can configure a separate provider alias
using its `secret` for rules deployment. Keep the alias separate from the provider
that manages the key to avoid a dependency cycle.

## Import

Use `organization_id/project_id/key_id`, all plain ULIDs:

```sh
terraform import terrascale_api_key.reader 01ARZ3NDEKTSV4RRFFQ69G5FAV/01ARZ3NDEKTSV4RRFFQ69G5FAW/01ARZ3NDEKTSV4RRFFQ69G5FAZ
```

Import produces a warning and a null `secret`. It does not rotate the key.
