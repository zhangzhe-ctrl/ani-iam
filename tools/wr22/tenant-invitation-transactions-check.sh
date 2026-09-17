#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22TenantInvitationTransactions|TestWR22TenantRoleTransactions|TestWR22PlatformPasswordActionComponents)$'
bash tools/wr22/roles-formal.sh
