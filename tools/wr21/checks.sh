#!/bin/bash
set -euo pipefail
: "${WR21_RUN_DIR:?}"
test "$PWD" = "$WR21_RUN_DIR/source"
result=0
check() {
  name="$1"; directory="$2"; shift 2
  set +e
  (cd "$directory" && "$@") > "$WR21_RUN_DIR/$name.log" 2>&1
  code=$?
  set -e
  printf '%s %s\n' "$name" "$code" >> "$WR21_RUN_DIR/check-exits.txt"
  if test "$code" -ne 0; then result=1; fi
}
check iam-directed "$PWD" go test -count=1 ./internal/biz ./internal/data ./internal/service
check sdk-directed "$PWD/sdk" go test -count=1 ./grpcworkload
check ani-pkg-directed "$WR21_RUN_DIR/source-ani/repo/pkg" go test -count=1 ./adapters/iam ./iaminference
check gateway-directed "$WR21_RUN_DIR/source-ani/repo/services/ani-gateway" go test -count=1 ./internal/authz ./internal/middleware ./internal/router
check inference-directed "$WR21_RUN_DIR/source-ani/repo/services/inference-service" go test -count=1 ./internal/service ./internal/grpcapi ./internal/config
check inference-build "$WR21_RUN_DIR/source-ani/repo/services/inference-service" go build ./...
check envoy-directed "$WR21_RUN_DIR/source-ani/repo/services/envoy-authz-adapter" go test -count=1 ./...
check integration-compile "$PWD" go test -tags=integration -run '^$' ./tests/integration
cat "$WR21_RUN_DIR/check-exits.txt"
exit "$result"
