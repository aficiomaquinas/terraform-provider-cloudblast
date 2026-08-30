package provider

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccServerResource_FullFlow(t *testing.T) {
	if os.Getenv("CLOUDBLAST_API_TOKEN") == "" {
		t.Skip("CLOUDBLAST_API_TOKEN not set, skipping acceptance test")
	}

	pubKeyBytes, err := os.ReadFile(os.ExpandEnv("$HOME/.ssh/zammad_deploy.pub"))
	if err != nil {
		t.Skipf("Cannot read zammad_deploy.pub: %v", err)
	}
	pubKey := strings.TrimSpace(string(pubKeyBytes))

	keyResourceName := "cloudblast_ssh_key.deploy"
	serverResourceName := "cloudblast_server.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFullFlowConfig(pubKey),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(keyResourceName, "id"),
					resource.TestCheckResourceAttr(keyResourceName, "name", "deploy-key-test"),
					resource.TestCheckResourceAttrSet(serverResourceName, "id"),
					resource.TestCheckResourceAttr(serverResourceName, "hostname", "test-full-flow"),
					resource.TestCheckResourceAttr(serverResourceName, "template", "ubuntu-24.04"),
					resource.TestCheckResourceAttrSet(serverResourceName, "ipv4"),
					testAccCheckSSH(serverResourceName),
				),
			},
		},
	})
}

func testAccCheckSSH(serverResourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[serverResourceName]
		if !ok {
			return fmt.Errorf("not found: %s", serverResourceName)
		}

		ip := rs.Primary.Attributes["ipv4"]
		if ip == "" {
			return fmt.Errorf("no ipv4 address")
		}

		privKey := os.ExpandEnv("$HOME/.ssh/zammad_deploy")

		// Poll for SSH readiness — CloudBlast provisioning takes variable time
		var lastErr error
		for i := 0; i < 30; i++ {
			time.Sleep(10 * time.Second)

			cmd := exec.Command("ssh",
				"-o", "StrictHostKeyChecking=no",
				"-o", "ConnectTimeout=5",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "IdentitiesOnly=yes",
				"-i", privKey,
				fmt.Sprintf("root@%s", ip),
				"echo SSH_SUCCESS && whoami",
			)
			output, err := cmd.CombinedOutput()
			if err == nil && strings.Contains(string(output), "SSH_SUCCESS") {
				t := testing.T{}
				t.Logf("✅ SSH to %s succeeded after %d attempts: %s", ip, i+1, strings.TrimSpace(string(output)))
				return nil
			}
			lastErr = fmt.Errorf("attempt %d: %v — %s", i+1, err, strings.TrimSpace(string(output)))
		}

		return fmt.Errorf("SSH to %s failed after 30 attempts: %v", ip, lastErr)
	}
}

func testAccFullFlowConfig(pubKey string) string {
	return fmt.Sprintf(`
resource "cloudblast_ssh_key" "deploy" {
  name       = "deploy-key-test"
  public_key = %q
}

resource "cloudblast_server" "test" {
  plan_id     = 20
  location_id = 4
  template    = "ubuntu-24.04"
  hostname    = "test-full-flow"
  ssh_key_ids = tostring(cloudblast_ssh_key.deploy.id)
}
`, pubKey)
}
