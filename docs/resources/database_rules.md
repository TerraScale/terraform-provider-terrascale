---
page_title: "terrascale_database_rules Resource - TerraScale"
description: "Deploy versioned end-user rules for a TerraScale database."
---

# terrascale_database_rules (Resource)

Manages the complete rules set for one database. Missing operations are denied.
Requires `project_token` or `TERRASCALE_PROJECT_TOKEN` with `databases:read` and
`databases:write` for the selected project. Public project API keys are exchanged
for these database scopes.

```terraform
resource "terrascale_database_rules" "main" {
  project  = terrascale_project.app.id
  database = terrascale_database.main.name
  rules = jsonencode({
    tables = {
      todos = {
        read     = "row.owner_id = auth.uid"
        create   = "TRUE"
        update   = "row.owner_id = auth.uid"
        delete   = "row.owner_id = auth.uid"
        columns  = ["id", "owner_id", "title"]
        writable = ["title"]
        validate = ["length(new.title) BETWEEN 1 AND 200"]
        set = {
          create = {
            id       = "request.ulid"
            owner_id = "auth.uid"
          }
        }
      }
    }
  })
}
```

The table and its referenced columns must already exist. The API compiles every
condition against the current schema before deployment. Invalid rules produce
Terraform diagnostics naming each table, operation or field, problem code and
message. Deployment warnings and problems discovered during refresh appear as
warnings so existing invalid rules can be corrected.

## Schema

- `project` (String, Required): canonical uppercase project ULID. Changes replace
  the resource.
- `database` (String, Required): database name matching
  `^[a-z][a-z0-9_-]{0,62}$`. Changes replace the resource.
- `rules` (String, Required): JSON object containing `tables`, up to 256 KiB.
  Key order and whitespace never cause a deployment. Optional fields omitted or
  set to null have the same meaning. Use `jsonencode` or `file` to supply JSON.
  Unknown fields and incorrectly typed values fail validation.
- `version` (String, Read-Only): current rules version ULID, read from the API on
  refresh and changed by each successful deployment.

Creation uses `If-None-Match: *` and refuses to overwrite an existing rules set;
import existing rules first. Updates send `If-Match` with the version in state.
A concurrent external deployment after planning fails with
`rules changed outside Terraform; run terraform refresh`. Refresh reads both the
live JSON and version, exposing outside changes in the plan. An outside deployment
of identical JSON is visible as a version change during refresh without requiring
another deployment.

Deletion deploys `{"tables":{}}` with `If-Match` from state, denying every end-user
operation. It retains rules version history and does not delete the database or
its rows. An unknown database or absent rules removes the resource from state;
other errors preserve state and fail. After deletion, recreating this resource
requires importing the remaining empty rules set first.

## Import

Use `project/database`, with a project ULID and database name:

```sh
terraform import terrascale_database_rules.main 01ARZ3NDEKTSV4RRFFQ69G5FAW/main
```

Import reads the current rules and version. Match the configuration to the live
rules before applying. Keep one rules resource per database: every deployment
replaces the complete rules set.
