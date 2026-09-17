# Each formal environment owns fixed private filenames; use a fresh run for
# Bootstrap recovery so no prior credentials or evidence are overwritten.
export WR23_FORMAL_TEST_PATTERN='^TestWR23ResumeFormalLifecycleChain$'
bash -euo pipefail tools/wr23-resume/formal-directed.sh
python3 - <<'PYGATE'
import json,os
from pathlib import Path
run=Path(os.environ['WR23_RESUME_RUN_DIR'])
for name in ['formal-chain-results.json']:
    proof=json.loads((run/name).read_text())
    assert proof['result']=='pass',name
PYGATE
# Recheck the only failed aggregate package after its reviewed raw registry pin
# changes; retain the earlier aggregate's successful packages as separate proof.
if go test -count=1 ./tests > "$WR23_RESUME_RUN_DIR/private/iam-registry-pin-recheck.raw.log" 2>&1; then code=0; else code=$?; fi
printf 'iam-registry-pin-recheck %s\n' "$code" >> "$WR23_RESUME_RUN_DIR/directed-checks.exit"
exit "$code"
