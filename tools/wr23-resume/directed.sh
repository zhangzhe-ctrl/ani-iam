python3 tools/wr23-resume/owners-generation.py
python3 tools/wr23-resume/generation.py
check() {
 name=$1; dir=$2; shift 2
 date -u +%FT%TZ > "$WR23_RESUME_RUN_DIR/$name.started"
 if (cd "$dir" && "$@") > "$WR23_RESUME_RUN_DIR/private/$name.raw.log" 2>&1; then code=0; else code=$?; fi
 printf '%s %s\n' "$name" "$code" >> "$WR23_RESUME_RUN_DIR/directed-checks.exit"
 date -u +%FT%TZ > "$WR23_RESUME_RUN_DIR/$name.finished"
 printf 'WR23 %s exit=%s\n' "$name" "$code"
 return "$code"
}
check core-publisher "$WR23_RESUME_RUN_DIR/source-ani/repo/pkg" go test -race -count=1 ./adapters/tenantlifecycle -run 'LifecycleStream|NATSSigning'
check core-runtime "$WR23_RESUME_RUN_DIR/source-ani/repo/services/ani-gateway" go test -count=1 . -run 'CoreSnapshot|RuntimeAdmin|GatewayPublicListener'
check publisher-vet "$WR23_RESUME_RUN_DIR/source-ani/repo/pkg" go vet ./adapters/tenantlifecycle

check dlq-unit "$WR23_RESUME_RUN_DIR/source" go test -race -count=1 ./internal/biz ./internal/service ./internal/data ./cmd/server -run 'CoreDLQ|CoreBroker'
check dlq-gateway "$WR23_RESUME_RUN_DIR/source-ani/repo/services/ani-gateway" go test -count=1 ./internal/authz ./internal/router -run 'CoreDLQ|CoreLifecycle'
