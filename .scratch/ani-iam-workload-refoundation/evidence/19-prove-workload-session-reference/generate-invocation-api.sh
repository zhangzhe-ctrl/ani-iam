bash tools/wr19/generate.sh
(cd api; GOWORK=off go mod tidy; GOWORK=off go test ./...)
tar -czf "$WR19_RUN_DIR/api-module-lock.tar.gz" api/go.mod api/go.sum
