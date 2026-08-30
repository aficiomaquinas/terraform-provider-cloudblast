terraform {
  required_providers {
    cloudblast = {
      source  = "aficiomaquinas/cloudblast"
      version = "~> 0.1"
    }
  }
}

provider "cloudblast" {}

# Step 1: Create SSH key
resource "cloudblast_ssh_key" "deploy" {
  name       = "tf-provider-integration-test"
  public_key = file("~/.ssh/zammad_deploy.pub")
}

# Step 2: Create server with SSH key
resource "cloudblast_server" "test" {
  plan_id     = 20
  location_id = 4
  template    = "ubuntu-24.04"
  hostname    = "tf-integration-test"
  ssh_key_ids = tostring(cloudblast_ssh_key.deploy.id)
}

# Step 3: Wait for SSH
resource "null_resource" "ssh_test" {
  depends_on = [cloudblast_server.test]

  provisioner "local-exec" {
    command = <<-EOT
      echo "⏳ Waiting 60s for server provisioning..."
      sleep 60
      echo "🔑 Testing SSH with zammad_deploy key..."
      for i in 1 2 3; do
        if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
          -o UserKnownHostsFile=/dev/null \
          -i ~/.ssh/zammad_deploy \
          root@${cloudblast_server.test.ipv4} "echo SSH_SUCCESS && whoami && cat /root/.ssh/authorized_keys 2>/dev/null | head -3"; then
          echo "✅ SSH test PASSED"
          exit 0
        fi
        echo "Attempt $i failed, retrying in 15s..."
        sleep 15
      done
      echo "❌ SSH test FAILED after 3 attempts"
      exit 1
    EOT
  }
}

output "key_id" { value = cloudblast_ssh_key.deploy.id }
output "server_ip" { value = cloudblast_server.test.ipv4 }
output "server_id" { value = cloudblast_server.test.id }
