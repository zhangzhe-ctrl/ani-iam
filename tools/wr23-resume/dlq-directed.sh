export WR23_BROKER_COMPONENTS=0 WR23_BROKER_PG=1 WR23_BROKER_NATS=0
export WR23_PG_TEST_PATTERN='^(TestWR23ResumeBrokerPostgres|TestNoRLSPersistenceFoundation)$'
bash -euo pipefail tools/wr23-resume/broker-directed.sh
check() {
 name=$1; dir=$2; shift 2
 if (cd "$dir" && "$@") > "$WR23_RESUME_RUN_DIR/private/$name.raw.log" 2>&1; then code=0; else code=$?; fi
 printf '%s %s\n' "$name" "$code" >> "$WR23_RESUME_RUN_DIR/directed-checks.exit"
 return "$code"
}
export GOMEMLIMIT=1536MiB
check dlq-unit . go test -race -count=1 ./internal/biz ./internal/service ./internal/data ./cmd/server -run 'CoreDLQ|CoreBroker'
check dlq-gateway ../source-ani/repo/services/ani-gateway go test -race -count=1 ./internal/authz ./internal/router -run 'CoreDLQ|CoreLifecycle'
check dlq-vet . go vet ./internal/biz ./internal/data ./internal/service ./cmd/server
