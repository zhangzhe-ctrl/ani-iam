#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalInvitedAccount|TestWR22FormalInvitationBootstrap)$'
bash tools/wr22/roles-formal.sh
