package main

import (
	"context"
	"log"

	"github.com/TerraScale/terraform-provider-terrascale/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version string = 0

func main() {
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/terrascale/terrascale",
	}); err != nil {
		log.Fatal(err)
	}
}
