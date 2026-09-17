set -euo pipefail
export WR23_SHADOW_KIND=rehearsal
export WR23_RESOURCE_DIAGNOSTICS=1
PYTHONPYCACHEPREFIX="$WR23_RESUME_RUN_DIR/private/pycache" python3 -m py_compile tools/wr23-resume/remote-run.py tools/wr23-resume/formal.py tools/wr23-resume/collect-run.py
bash -n tools/wr23-resume/run.sh tools/wr23-resume/formal-directed.sh tools/wr23-resume/shadow-directed.sh tools/wr23-resume/formal-enforced-directed.sh tools/wr23-resume/formal-enforced-session-directed.sh
export WR23_FORMAL_TEST_PATTERN='^TestWR23ResumeFormalShadow$'
source tools/wr23-resume/formal-directed.sh
