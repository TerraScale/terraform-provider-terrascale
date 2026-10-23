resource "terrascale_api_key" "reader" {
  organization_id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
  project_id      = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
  name            = "Document reader"
  scopes          = ["documents:read"]
  database_name   = "main"
}

# Sensitive hides display; encrypt and restrict access to Terraform state.
output "reader_secret" {
  value     = terrascale_api_key.reader.secret
  sensitive = true
}
