#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22PlatformPasswordActionComponents|TestWR22FormalBossIdentityLink|TestPostgresPassword(Action|Reset|Setup).*)$'
bash tools/wr22/roles-formal.sh
