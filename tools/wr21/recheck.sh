#!/bin/bash
set -euo pipefail
: "${WR21_RUN_DIR:?}"
test "$PWD" = "$WR21_RUN_DIR/source"
mkdir -p "$WR21_RUN_DIR/private/gates"
result=0
check() {
 name="$1"; directory="$2"; shift 2
 set +e
 (cd "$directory" && "$@") > "$WR21_RUN_DIR/private/gates/$name.log" 2>&1
 code=$?
 set -e
 printf '%s %s\n' "$name" "$code" >> "$WR21_RUN_DIR/gates-exits.txt"
 if test "$code" -ne 0; then result=1; fi
}
ani="$WR21_RUN_DIR/source-ani/repo"
check sdk-generated "$ani" python3 scripts/validate_sdk_alpha.py
check iam-contracts "$PWD" go test -count=1 ./tests/contracts ./internal/conf ./internal/server ./cmd/server
export WR21_DOCKER_NETWORK="ani-iam-$(basename "$WR21_RUN_DIR")"
docker network create --label ani.goal=wr21 --label "ani.run_id=$(basename "$WR21_RUN_DIR")" "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/gates-network.id"
trap 'docker network rm "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/gates-network-stopped.log" 2>&1 || true' EXIT
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
check real-postgres "$PWD" go test -tags=integration -count=1 -timeout=10m ./tests/integration -run '^(TestTenantWorkload|TestAPIKey|TestRemovingTenantWorkload|TestTenantAuthorizationAuditFailure)'
check composite-fk "$PWD" go test -tags=integration -count=1 -timeout=4m ./tests/integration -run '^TestNoRLSPersistenceFoundation$/composite_foreign_keys'
check wr19-invocation-regression "$PWD" go test -tags=integration -count=1 -timeout=5m ./tests/integration -run '^TestFormalWorkloadInvocation$'
cat "$WR21_RUN_DIR/gates-exits.txt"
exit "$result"
