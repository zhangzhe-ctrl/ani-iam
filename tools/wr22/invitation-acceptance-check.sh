#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalInvitationAcceptance|TestWR22FormalPlatformInvitations|TestWR22FormalTenantInvitations)$'
bash tools/wr22/roles-formal.sh
