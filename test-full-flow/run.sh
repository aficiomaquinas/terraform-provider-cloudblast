#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
terraform init -upgrade
echo ""
echo "=== PLAN ==="
terraform plan
echo ""
echo "=== APPLY ==="
terraform apply -auto-approve
echo ""
echo "=== DONE ==="
