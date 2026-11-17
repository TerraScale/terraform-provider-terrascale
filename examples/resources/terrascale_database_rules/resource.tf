terraform {
  required_providers {
    terrascale = {
      source = "terrascale/terrascale"
    }
  }
}

provider "terrascale" {}

resource "terrascale_database_rules" "main" {
  project  = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
  database = terrascale_database.main.id
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
