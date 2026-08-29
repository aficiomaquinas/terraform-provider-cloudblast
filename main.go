
package main

import (
	"context"
	"flag"
	"log"

	"github.com/aficiomaquinas/terraform-provider-cloudblast/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/aficiomaquinas/cloudblast",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New("dev"), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
