---
page_title: "TerraScale Provider"
description: "Manage TerraScale projects, databases, collections and project API keys."
---

# TerraScale Provider

The TerraScale provider uses the public Go SDK: projects and API keys call
`https://auth.terrascale.tech/v1/terrascale`; databases and collections call
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
  access token for collection operations. Defaults to `TERRASCALE_PROJECT_TOKEN`. It must authorize the
  collection's project: `documents:read` for reads and `documents:write` for
  creation/deletion. Both scopes are needed for the complete collection lifecycle.
  Public keys are exchanged for cached project tokens requesting only these two
  scopes; the server derives the project from the key's stored metadata. Project
  JWTs are used directly.

Database operations exchange that user token for a cached project access token
with only `databases:read` and `databases:write`. No additional credential is
needed. An expired or rejected user token requires supplying a current token.

An explicitly configured value overrides its environment default. Missing
credentials are diagnosed when the corresponding resource is used. Credentials
must not contain whitespace or controls. Never use internal peer credentials.
Use provider aliases for collections in different projects, with separate
existing credentials. Do not make provider configuration depend on a resource
managed by that same provider. A default-provider API key can configure a separate
collection provider alias, as shown in the [collection example](resources/collection.md).

API-key secrets persist in state. Sensitive marking hides display only: encrypt
and restrict access to state and plan files. Imports cannot recover existing key
secrets. Database and collection destruction removes data; consider
`lifecycle.prevent_destroy`.
