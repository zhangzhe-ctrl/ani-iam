#!/bin/bash
set -euo pipefail
export WR22_FORMAL_TESTS='^TestWR22Formal(Tenant(Membership|AdministratorLogin)|Platform(Password|Membership))$'
bash tools/wr22/roles-formal.sh
