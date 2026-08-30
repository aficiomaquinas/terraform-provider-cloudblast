#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
export CLOUDBLAST_API_TOKEN="ImVXZsN4cm6DtwZgRVBapY7Mb5JoAtGn5wi3aUy6aYT9pb0xYq37QoBcuSZ1"
terraform destroy -auto-approve
