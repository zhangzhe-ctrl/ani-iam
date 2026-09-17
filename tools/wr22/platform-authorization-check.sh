#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
export WR22_FORMAL_TESTS='^TestWR22Formal(BossOIDCAndSessions|PlatformOnlineAuthorization)$'
bash tools/wr22/roles-formal.sh
