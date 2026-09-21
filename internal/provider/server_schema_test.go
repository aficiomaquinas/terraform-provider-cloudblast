// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccServerResource_Schema wires the full resource schema through the
// provider (PlanOnly, no API calls). It verifies the failover attributes are
// correctly registered and that no base attributes were broken.
func TestAccServerResource_Schema(t *testing.T) {
	// PlanOnly: never calls the CloudBlast API; any non-empty token works.
	t.Setenv("CLOUDBLAST_API_TOKEN", "dummy-token-schema-test-only")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				Config: `
resource "cloudblast_server" "test" {
  plan_id               = 20
  location_id           = 4
  template              = "ubuntu-24.04"
  hostname              = "schema-smoke"
  failover_location_ids = [1, 2, 3]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudblast_server.test", "plan_id", "20"),
					resource.TestCheckResourceAttr("cloudblast_server.test", "location_id", "4"),
					resource.TestCheckResourceAttr("cloudblast_server.test", "failover_location_ids.#", "3"),
					resource.TestCheckResourceAttr("cloudblast_server.test", "failover_location_ids.0", "1"),
					resource.TestCheckResourceAttr("cloudblast_server.test", "failover_location_ids.1", "2"),
					resource.TestCheckResourceAttr("cloudblast_server.test", "failover_location_ids.2", "3"),
					resource.TestMatchResourceAttr("cloudblast_server.test", "effective_location_id", regexp.MustCompile(`^\d+$`)),
				),
			},
		},
	})
}
