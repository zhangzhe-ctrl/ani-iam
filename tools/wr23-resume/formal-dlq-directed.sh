export WR23_FORMAL_TEST_PATTERN='^TestWR23ResumeFormalDLQ$'
bash -euo pipefail tools/wr23-resume/formal-directed.sh
python3 - <<'PYGATE'
import json,os
from pathlib import Path
proof=json.loads((Path(os.environ['WR23_RESUME_RUN_DIR'])/'formal-dlq-results.json').read_text())
assert proof['result']=='pass' and proof['complete_dlq_gate'] and not proof['remaining'],'DLQ full fault matrix remains not_verified'
PYGATE
