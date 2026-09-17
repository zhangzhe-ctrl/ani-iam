#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalTenantBootstrapRecovery|TestWR22FormalTenantAdminRecovery|TestWR22FormalInvitationAcceptance)$'
bash tools/wr22/roles-formal.sh
