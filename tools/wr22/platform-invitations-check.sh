#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalPlatformInvitations|TestWR22FormalTenantInvitations|TestWR22TenantInvitationTransactions|TestWR22FormalPlatformRoleAndAccessMutations)$'
bash tools/wr22/roles-formal.sh
