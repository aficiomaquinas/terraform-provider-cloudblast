terraform {
  required_providers {
    cloudblast = {
      source = "aficiomaquinas/cloudblast"
    }
  }
}

provider "cloudblast" {
  # API token can also be set via CLOUDBLAST_API_TOKEN env var
  # api_token = "cb_live_..."
}
