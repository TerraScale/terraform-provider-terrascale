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
  scopes          = ["documents:read"]
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
- `scopes` (Set of String, Required): nonempty subset of `documents:read` and
  `documents:write`.
- `database_id` (String, Optional): plain database ULID limiting the key to that
  database. Omit to reach all databases in the project.
- `id` (String, Read-Only): API key ULID.
- `secret` (String, Sensitive, Read-Only): one-time secret retained across refresh.
  Null for imports.

Every configuration change replaces the key and revokes the old key. Deletion
revokes immediately. `documents:read` also permits collection reads;
`documents:write` permits collection creation and deletion. The collection
resource needs both scopes for its full lifecycle, including read-before-delete
identity checks. A read-only key cannot create or delete collections.
If creation has an uncertain outcome, inspect the project's keys before retrying;
the provider does not retry uncertain mutations or recover the one-time secret.

## Import

Use `organization_id/project_id/key_id`, all plain ULIDs:

```sh
terraform import terrascale_api_key.reader 01ARZ3NDEKTSV4RRFFQ69G5FAV/01ARZ3NDEKTSV4RRFFQ69G5FAW/01ARZ3NDEKTSV4RRFFQ69G5FAZ
```

Import produces a warning and a null `secret`. It does not rotate the key.
