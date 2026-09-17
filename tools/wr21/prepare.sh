#!/bin/bash
set -euo pipefail
result=0
for stage in generate recheck; do
 set +e
 bash "tools/wr21/$stage.sh"
 code=$?
 set -e
 printf '%s %s\n' "$stage" "$code" >> "$WR21_RUN_DIR/phase-exits.txt"
 if test "$code" -ne 0; then result=1; fi
done
exit "$result"
