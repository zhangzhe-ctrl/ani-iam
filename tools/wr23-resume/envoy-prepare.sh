set -euo pipefail
check formal-adapter-build ../source-ani/repo/services/envoy-authz-adapter go build -o "$WR23_RUN_DIR/private/envoy-authz-adapter" .
check formal-inference-build ../source-ani/repo/services/inference-service go build -o "$WR23_RUN_DIR/private/inference-service" .
asset="$WR23_RESUME_CACHE/envoy-1.38.4-linux-x86_64"
if ! test -f "$asset"; then
 curl --fail --location --retry 2 --connect-timeout 15 --max-time 240 'https://github.com/envoyproxy/envoy/releases/download/v1.38.4/envoy-1.38.4-linux-x86_64' --output "$WR23_RUN_DIR/private/envoy.part"
 printf '%s  %s\n' c994c452de131f59c9ec9f4a2fffcc65039f250a38b6279870bb95dac21db0fa "$WR23_RUN_DIR/private/envoy.part" | sha256sum --check -
 test "$(stat -c %s "$WR23_RUN_DIR/private/envoy.part")" = 102051288
 mv "$WR23_RUN_DIR/private/envoy.part" "$asset"
fi
printf '%s  %s\n' c994c452de131f59c9ec9f4a2fffcc65039f250a38b6279870bb95dac21db0fa "$asset" | sha256sum --check -
cp "$asset" "$WR23_RUN_DIR/private/envoy"
chmod 700 "$WR23_RUN_DIR/private/envoy"
"$WR23_RUN_DIR/private/envoy" --version > "$WR23_RUN_DIR/envoy-version.txt"
sha256sum "$WR23_RUN_DIR/private/envoy" > "$WR23_RUN_DIR/envoy-binary.sha256"
