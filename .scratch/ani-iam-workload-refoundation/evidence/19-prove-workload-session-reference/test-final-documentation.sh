mkdir "$WR19_RUN_DIR/private/bin"
ln -s /usr/bin/python3 "$WR19_RUN_DIR/private/bin/python"
export PATH="$WR19_RUN_DIR/private/bin:$PATH"
cd "$WR19_RUN_DIR/source-ani/repo"
make validate-doc-entrypoints GO_CACHE_ENV=
