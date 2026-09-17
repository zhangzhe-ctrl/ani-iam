#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalInvitationBootstrap|TestWR22FormalInvitationAcceptance|TestWR22FormalTenantBootstrapRecovery)$'
bash tools/wr22/roles-formal.sh
