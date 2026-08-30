#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
export CLOUDBLAST_API_TOKEN="ImVXZsN4cm6DtwZgRVBapY7Mb5JoAtGn5wi3aUy6aYT9pb0xYq37QoBcuSZ1"
terraform init -upgrade
echo ""
echo "=== PLAN ==="
terraform plan
echo ""
echo "=== APPLY ==="
terraform apply -auto-approve
echo ""
echo "=== DONE ==="
