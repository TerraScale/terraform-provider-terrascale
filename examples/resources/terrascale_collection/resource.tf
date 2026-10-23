# Use an existing project credential with documents:read and documents:write.
resource "terrascale_collection" "users" {
  project_id    = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
  database_name = "main"
  name          = "users"

  lifecycle {
    prevent_destroy = true
  }
}
