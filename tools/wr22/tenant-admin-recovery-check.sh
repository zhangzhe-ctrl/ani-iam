#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^(TestWR22FormalTenantAdminRecovery|TestWR22FormalPlatformRoleAndAccessMutations)$'
bash tools/wr22/roles-formal.sh
