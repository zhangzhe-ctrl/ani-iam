cd "$WR19_RUN_DIR/source-ani/repo"
export PYTHONDONTWRITEBYTECODE=1
python3 services/ani-gateway/tools/wr19_operation_registry.py
sha256sum services/ani-gateway/internal/authz/zz_generated_workload_reference.go > "$WR19_RUN_DIR/ani-generation.sha256"
python3 services/ani-gateway/tools/wr19_operation_registry.py --check
tar -czf "$WR19_RUN_DIR/ani-generated.tar.gz" services/ani-gateway/internal/authz/zz_generated_workload_reference.go
