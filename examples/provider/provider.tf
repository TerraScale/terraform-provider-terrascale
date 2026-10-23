terraform {
  required_providers {
    terrascale = {
      source  = "terrascale/terrascale"
      version = "0.0.1"
    }
  }
}

# Existing credentials: TERRASCALE_ACCESS_TOKEN and TERRASCALE_PROJECT_TOKEN.
provider "terrascale" {}
