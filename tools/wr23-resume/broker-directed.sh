set -euo pipefail
export WR23_BROKER_COMPONENTS="${WR23_BROKER_COMPONENTS:-1}" WR23_BROKER_PG="${WR23_BROKER_PG:-1}" WR23_BROKER_NATS="${WR23_BROKER_NATS:-0}"
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
if [ "${WR23_BROKER_COMPONENTS:-0}" = 1 ]; then
check broker-sql-generation . go test -count=1 ./internal/biz ./internal/data ./internal/service ./cmd/server -run 'CoreNATS|CoreBroker|CoreBootstrap|CoreProjection|CoreSnapshot|CoreLifecycle|LifecycleObservation'

if [ "${WR23_BROKER_HTTP:-0}" = 1 ]; then
check broker-sdk-http ./sdk go test -race -count=1 ./grpcworkload -run HTTP
fi
check broker-runtime-vet . go vet ./cmd/server ./internal/data ./internal/biz
fi
if [ "${WR23_BROKER_PG:-0}" = 1 ] || [ "${WR23_BROKER_NATS:-1}" = 1 ]; then
 export WR23_RUN_DIR="$WR23_RESUME_RUN_DIR"
 export WR23_DOCKER_NETWORK="ani-iam-$(basename "$WR23_RUN_DIR")"
 export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
 check broker-integration-build . go test -c -tags=integration -o "$WR23_RUN_DIR/private/broker-tests" ./tests/integration
 docker pull postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c > "$WR23_RUN_DIR/private/postgres-image-pull.log" 2>&1
 docker image inspect postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c --format '{{.Architecture}} {{index .RepoDigests 0}}' > "$WR23_RUN_DIR/private/postgres-image.txt"
 docker network create --driver bridge --label ani.goal=wr23 --label "ani.run_id=$(basename "$WR23_RUN_DIR")" "$WR23_DOCKER_NETWORK" > "$WR23_RUN_DIR/network.id"
 trap 'docker network rm "$WR23_DOCKER_NETWORK" > "$WR23_RUN_DIR/network-cleanup.log" 2>&1' EXIT
 export GOMEMLIMIT=768MiB
 if [ "${WR23_BROKER_PG:-0}" = 1 ]; then
 check broker-postgres tests/integration "$WR23_RUN_DIR/private/broker-tests" -test.v -test.count=1 -test.timeout=10m -test.run="${WR23_PG_TEST_PATTERN:-^TestWR23ResumeBrokerPostgres$}"
 fi
 if [ "${WR23_BROKER_NATS:-1}" = 1 ]; then
  docker pull docker.io/library/nats@sha256:065e8355c20a5575b3c77224be1855e8103fd148b68fba05130b9b8ddfa40ccc > "$WR23_RUN_DIR/private/nats-image-pull.log" 2>&1
  docker image inspect docker.io/library/nats@sha256:065e8355c20a5575b3c77224be1855e8103fd148b68fba05130b9b8ddfa40ccc --format '{{.Architecture}} {{.Id}} {{json .RepoDigests}}' > "$WR23_RUN_DIR/private/nats-image.txt"
  check broker-nats tests/integration "$WR23_RUN_DIR/private/broker-tests" -test.v -test.count=1 -test.timeout=5m -test.run='^TestWR23ResumeNATSTransport$'
 fi
fi
