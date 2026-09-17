#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^TestWR22FormalTenantAudit$'
bash tools/wr22/roles-formal.sh
