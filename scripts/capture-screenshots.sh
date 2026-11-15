#!/usr/bin/env bash
# Capture the full Veylo screenshot set into docs/screenshots/.
# Requires: infra + API + Web running (make infra-up && make dev in another shell).
set -euo pipefail
cd "$(dirname "$0")/.."

set -a; [ -f .env ] && source .env; set +a

cd tests/e2e
node capture.mjs
