---
page_title: "TerraScale Provider"
description: "Manage TerraScale projects, databases, database rules and project API keys."
---

# TerraScale Provider

The TerraScale provider uses the public Go SDK: projects and API keys call
`https://auth.terrascale.tech/v1/terrascale`; databases call
`https://api.terrascale.tech/v1/projects`. It manages existing-organization resources;
it does not create organizations or provision infrastructure.

## Example

```terraform
terraform {
  required_providers {
    terrascale = {
      source  = "terrascale/terrascale"
      version = "0.0.1"
    }
  }
}

provider "terrascale" {}
```

## Schema

### Optional

- `access_token` (String, Sensitive): ts-cli user access token from ts-auth for project,
  database and API-key management. Defaults to `TERRASCALE_ACCESS_TOKEN`.
- `project_token` (String, Sensitive): public ts-auth project API key or project
  access token for database rules. Defaults to `TERRASCALE_PROJECT_TOKEN`. Keys are exchanged for
  `databases:read` and `databases:write`; project JWTs are used directly.

Database operations exchange that user token for a cached project access token
with only `databases:read` and `databases:write`. No additional credential is
needed. An expired or rejected user token requires supplying a current token.

An explicitly configured value overrides its environment default. Missing
credentials are diagnosed when the corresponding resource is used. Credentials
must not contain whitespace or controls. Never use internal peer credentials.

API-key secrets persist in state. Sensitive marking hides display only: encrypt
and restrict access to state and plan files. Imports cannot recover existing key
secrets. Database destruction removes data; consider
`lifecycle.prevent_destroy`.
