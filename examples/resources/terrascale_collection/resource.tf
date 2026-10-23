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
