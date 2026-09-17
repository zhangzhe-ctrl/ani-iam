#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/../.."
exec python3 tools/wr23-resume/formal.py "$@"
