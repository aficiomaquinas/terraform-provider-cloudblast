# Terraform Provider CloudBlast

A Terraform provider for managing [CloudBlast](https://cloudblast.io) cloud infrastructure — VPS servers, SSH keys, security groups, and firewall rules.

Built with the [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework). Wire-compatible with OpenTofu.

## Resources

| Resource | Description |
|---|---|
| `cloudblast_server` | VPS server (create, read, delete, import) |
| `cloudblast_ssh_key` | SSH key management |
| `cloudblast_security_group` | Security group (firewall container) |
| `cloudblast_firewall_rule` | Individual firewall rule |

## Data Sources

| Data Source | Description |
|---|---|
| `cloudblast_plans` | List available server plans |
| `cloudblast_locations` | List data center locations |
| `cloudblast_templates` | List OS templates per location |
| `cloudblast_account` | Account info and balance |

## Quick Start

```hcl
terraform {
  required_providers {
    cloudblast = {
      source = "aficiomaquinas/cloudblast"
    }
  }
}

provider "cloudblast" {
  # Or set CLOUDBLAST_API_TOKEN env var
}

resource "cloudblast_ssh_key" "deploy" {
  name       = "deploy-key"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "cloudblast_server" "web" {
  plan_id     = 19
  location_id = 2
  template    = "ubuntu-24.04"
  hostname    = "web-01"
  ssh_key_ids = cloudblast_ssh_key.deploy.id
}
```

## Configuration

| Attribute | Env Var | Description |
|---|---|---|
| `api_token` | `CLOUDBLAST_API_TOKEN` | API token (required) |
| `endpoint` | `CLOUDBLAST_API_URL` | API base URL (optional, default: `https://console.cloudblast.io/api/v2`) |

## Locations

| ID | Code | Location | Status |
|---|---|---|---|
| 1 | `nl` | Amsterdam, NL | Out of stock |
| 2 | `usa` | Salt Lake City, USA | Available |
| 4 | `hk` | Hong Kong, HK | Available |

## Development

### Requirements

- [Go](https://golang.org/doc/install) >= 1.24
- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0

### Build

```shell
go build -o terraform-provider-cloudblast
```

### Local Testing (dev overrides)

Add to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "aficiomaquinas/cloudblast" = "/path/to/terraform-provider-cloudblast"
  }
  direct {}
}
```

Then run `terraform plan` directly (skip `terraform init`).

### Acceptance Tests

```shell
export CLOUDBLAST_API_TOKEN="cb_live_..."
export TF_ACC=1
go test -v -cover ./internal/provider/
```

### Generate Documentation

```shell
make generate
```

## License

MIT
