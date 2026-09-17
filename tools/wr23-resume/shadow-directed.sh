set -euo pipefail
export WR23_SHADOW_KIND=accepted12h30
export WR23_FORMAL_TEST_PATTERN='^TestWR23ResumeFormalShadow$'
export WR23_FORMAL_TIMEOUT=25h
source tools/wr23-resume/formal-directed.sh
