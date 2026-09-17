#!/bin/bash
set -euo pipefail
: "${WR21_RUN_DIR:?}"
curl --fail --location --retry 2 --connect-timeout 15 --max-time 240 'https://github.com/envoyproxy/envoy/releases/download/v1.38.4/envoy-1.38.4-linux-x86_64' --output "$WR21_RUN_DIR/private/envoy.part"
printf '%s  %s\n' c994c452de131f59c9ec9f4a2fffcc65039f250a38b6279870bb95dac21db0fa "$WR21_RUN_DIR/private/envoy.part" | sha256sum --check -
test "$(stat -c %s "$WR21_RUN_DIR/private/envoy.part")" = 102051288
mv "$WR21_RUN_DIR/private/envoy.part" "$WR21_RUN_DIR/private/envoy"
chmod 700 "$WR21_RUN_DIR/private/envoy"
"$WR21_RUN_DIR/private/envoy" --version > "$WR21_RUN_DIR/envoy-version.txt"
sha256sum "$WR21_RUN_DIR/private/envoy" > "$WR21_RUN_DIR/envoy-binary.sha256"
