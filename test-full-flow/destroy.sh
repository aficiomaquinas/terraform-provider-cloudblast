#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
terraform destroy -auto-approve
