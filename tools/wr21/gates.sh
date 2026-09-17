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
check directed "$PWD" bash tools/wr21/checks.sh
check envoy-mtls "$ani/services/envoy-authz-adapter" go test -tags=integration -count=1 ./...
check iam-contracts "$PWD" go test -count=1 ./tests/contracts ./internal/conf ./internal/server ./cmd/server
check iam-vet "$PWD" go vet ./internal/... ./cmd/server
check sdk-race "$PWD/sdk" go test -race -count=1 ./grpcworkload
check iam-race "$PWD" go test -race -count=1 ./internal/biz ./internal/data ./internal/service
check gateway-race "$ani/services/ani-gateway" go test -race -count=1 ./internal/authz ./internal/middleware ./internal/router
check envoy-race "$ani/services/envoy-authz-adapter" go test -race -count=1 ./...
check owner-race "$ani/services/inference-service" go test -race -count=1 ./internal/grpcapi ./internal/service
check gateway-vet "$ani/services/ani-gateway" go vet ./...
check envoy-vet "$ani/services/envoy-authz-adapter" go vet ./...
check inference-vet "$ani/services/inference-service" go vet ./...
for script in generate_gateway_authz_test validate_gateway_authz_drift validate_core_gateway_authz_routes validate_services_boundary validate_services_contract_test validate_services_contract validate_services_route_contract_test validate_services_route_contract wr21_inference_contract_test validate_spec_split_contract validate_component_imports validate_inference_legacy_control_plane validate_doc_entrypoints validate_doc_entrypoints_test; do
 check "$script" "$ani" python3 "scripts/$script.py"
done
check core-yaml "$ani" python3 scripts/validate_yaml.py api/openapi/v1.yaml
check services-yaml "$ani" python3 scripts/validate_yaml.py api/openapi/services/v1.yaml
export WR21_DOCKER_NETWORK="ani-iam-$(basename "$WR21_RUN_DIR")"
docker network create --label ani.goal=wr21 --label "ani.run_id=$(basename "$WR21_RUN_DIR")" "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/gates-network.id"
trap 'docker network rm "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/gates-network-stopped.log" 2>&1 || true' EXIT
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
check real-postgres "$PWD" go test -tags=integration -count=1 -timeout=10m ./tests/integration -run '^(TestTenantWorkload|TestAPIKey|TestRemovingTenantWorkload|TestTenantAuthorizationAuditFailure)'
check composite-fk "$PWD" go test -tags=integration -count=1 -timeout=4m ./tests/integration -run '^TestNoRLSPersistenceFoundation$/composite_foreign_keys'
check wr19-invocation-regression "$PWD" go test -tags=integration -count=1 -timeout=5m ./tests/integration -run '^TestFormalWorkloadInvocation$'
cat "$WR21_RUN_DIR/gates-exits.txt"
exit "$result"
