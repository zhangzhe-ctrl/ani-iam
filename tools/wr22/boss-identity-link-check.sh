#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^TestWR22Formal(BossIdentityLink|PlatformPassword)$'
bash tools/wr22/roles-formal.sh
