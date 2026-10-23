resource "terrascale_database" "main" {
  organization_id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
  project_id      = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
  name            = "main"
  home_region     = "gru-1"

  lifecycle {
    prevent_destroy = true
  }
}
