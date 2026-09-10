#!/usr/bin/env python3
"""WR-16 documentation audit only; does not run or assess IAM behavior."""
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path
from urllib.parse import unquote, urlsplit

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
EFFORT = ROOT / '.scratch/ani-iam-workload-refoundation'
baseline = json.loads((HERE / 'baseline.json').read_text())
checks = {}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None


def record(name, failures, **details):
    checks[name] = {'status': 'fail' if failures else 'pass', **details, 'failures': failures}


def field(text, name):
    match = re.search(r'^\*\*' + re.escape(name) + r':\*\* (.+)$', text, re.M)
    return match.group(1) if match else None


record('before_snapshot_integrity', [p for p, h in baseline['documents'].items()
       if digest(HERE / 'before' / p) != h], files=len(baseline['documents']))
record('protected_tracked_files_unchanged', [p for p, h in baseline['protected_tracked'].items()
       if digest(ROOT / p) != h], files=len(baseline['protected_tracked']),
       limitation='Does not assert pre/post hashes for pre-existing untracked files outside the snapshot.')
head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
record('head_unchanged', [] if head == baseline['head'] else [head], head=head)

def allowed_document(path):
    return (path in ('README.md', 'AGENTS.md', 'CLAUDE.md', 'CONTEXT.md',
                     'docs/agents/domain.md', 'docs/agents/issue-tracker.md', '.scratch/ani-iam-rebuild/spec.md')
            or path.startswith(('docs/adr/', 'docs/plans/', '.scratch/ani-iam-workload-refoundation/')))


changed_documents = [p for p, h in baseline['documents'].items() if digest(ROOT / p) != h]
record('snapshot_document_scope', [p for p in changed_documents if not allowed_document(p)],
       changed_documents=changed_documents)

issues = {int(p.name[:2]): p for p in (EFFORT / 'issues').glob('[0-9][0-9]-*.md')}
old_failures = []
for n in range(1, 16):
    path = issues[n]
    old = (HERE / 'before' / path.relative_to(ROOT)).read_text()
    current = path.read_text()
    # Only the added opening supersession block and first Status field may differ.
    stripped = re.sub(r'\n> \*\*Superseded[^\n]*\n(?:>[^\n]*\n)*\n', '\n', current, count=1)
    stripped = re.sub(r'^\*\*Status:\*\* .+$', '**Status:** ' + field(old, 'Status'), stripped, count=1, flags=re.M)
    if stripped != old or field(current, 'Status') != 'wontfix':
        old_failures.append(str(path.relative_to(ROOT)))
record('old_issue_bodies_preserved', old_failures, files=15)

statuses = {str(n): field(p.read_text(), 'Status') for n, p in sorted(issues.items())}
claimed = [n for n, status in statuses.items() if status == 'claimed']
record('single_documentation_issue_only', [] if claimed in (['16'], []) and statuses['16'] in ('claimed', 'resolved')
       and all(statuses[str(n)] in ('needs-info', 'needs-triage') for n in range(17, 30)) else [statuses],
       statuses=statuses, claimed=claimed)

dependencies = {}
graph_failures = []
for n in range(17, 30):
    text = issues[n].read_text()
    raw = field(text, 'Blocked by') or ''
    dependencies[n] = [int(v) for v in re.findall(r'\d+', raw)]
    if not dependencies[n] or any(d not in issues or d >= n for d in dependencies[n]):
        graph_failures.append(f'WR-{n}: invalid dependency {raw}')
    for heading in ['目标', '范围与非目标', '固定输入与工作面', '验收与验证', '恢复与停止', '结果']:
        if '## ' + heading not in text:
            graph_failures.append(f'WR-{n}: missing {heading}')
    if 'not_frozen' not in text or 'not_verified' not in text:
        graph_failures.append(f'WR-{n}: missing future evidence status')
for n, expected in {17: [16], 26: [25], 27: [25], 28: [26, 27], 29: [28]}.items():
    if dependencies[n] != expected:
        graph_failures.append(f'WR-{n}: milestone dependency mismatch')
record('new_issue_graph', graph_failures, files=13, dependencies=dependencies)

matrix = (EFFORT / 'capability-matrix.md').read_text()
rows = re.findall(r'^\| (C\d{2}|R\d{2}) \| (.+)$', matrix, re.M)
expected_ids = [f'C{n:02}' for n in range(1, 15)] + [f'R{n:02}' for n in range(1, 5)]
coverage_failures = [] if [r[0] for r in rows] == expected_ids else ['capability row set differs']
for identifier, row in rows:
    owners = re.findall(r'WR-(\d{2})', row)
    if not owners or any(int(n) not in issues for n in owners):
        coverage_failures.append(f'{identifier}: invalid issue link')
    if identifier.startswith('C') and 'not_verified' not in row:
        coverage_failures.append(f'{identifier}: product status prematurely changed')
record('capability_coverage', coverage_failures, mandatory_capabilities=14, post_m1_outcomes=4)

template_failures = []
for name in ['AGENTS.md', 'CLAUDE.md']:
    marker = '## Kratos 脚手架仓库规则'
    old = (HERE / 'before' / name).read_text().split(marker, 1)[1]
    new = (ROOT / name).read_text().split(marker, 1)[1]
    if old != new:
        template_failures.append(name)
record('kratos_template_preserved', template_failures)
rebuild = Path('.scratch/ani-iam-rebuild/spec.md')
marker = '本规格综合完整'
record('historical_rebuild_body_preserved', [] if (ROOT / rebuild).read_text().split(marker, 1)[1]
       == (HERE / 'before' / rebuild).read_text().split(marker, 1)[1] else [str(rebuild)])
trace = Path('docs/plans/plan-iam-decision-traceability.md')
def question_table(text):
    return text.split('## 2. 逐题映射', 1)[1].split('## 3.', 1)[0]
record('historical_300_answers_preserved', [] if question_table((ROOT / trace).read_text())
       == question_table((HERE / 'before' / trace).read_text()) else [str(trace)])

active = [ROOT / p for p in baseline['documents'] if not p.startswith('.scratch/')]
active += [EFFORT / n for n in ['spec.md', 'ticket-plan.md', 'capability-matrix.md', 'decisions.md']]
active += [issues[n] for n in range(16, 30)]
if (HERE / 'review.md').exists():
    active.append(HERE / 'review.md')
link_failures = []
whitespace_failures = []
count = 0
for path in active:
    raw = path.read_text()
    # Exclude examples in fenced code; validate local file/directory targets, not external URLs or anchors.
    text = re.sub(r'```.*?```', '', raw, flags=re.S)
    for target in re.findall(r'\[[^\]\n]*\]\(([^)\n]+)\)', text):
        target = target.strip().strip('<>')
        parsed = urlsplit(target)
        if parsed.scheme or not parsed.path:
            continue
        local = unquote(parsed.path)
        local = re.sub(r':\d+$', '', local)
        count += 1
        if not (path.parent / local).exists():
            link_failures.append(f'{path.relative_to(ROOT)} -> {target}')
    for line_no, line in enumerate(raw.splitlines(), 1):
        if line.endswith((' ', '\t')) and not line.endswith('  '):
            whitespace_failures.append(f'{path.relative_to(ROOT)}:{line_no}')
record('active_local_markdown_targets', link_failures, documents=len(active), links=count,
       limitation='Local target existence only; anchors and external URLs not checked. Historical before content is preserved, not re-linked.')
record('active_document_whitespace', whitespace_failures)
diff = subprocess.run(['git', 'diff', '--check'], cwd=ROOT, text=True, capture_output=True)
record('git_diff_check', [] if diff.returncode == 0 else [diff.stdout, diff.stderr])

result = {'scope': 'documentation_only', 'status': 'fail' if any(v['status'] == 'fail' for v in checks.values()) else 'pass',
          'product_runtime_status': 'not_verified', 'checks': checks,
          'current_document_hashes': {str(p.relative_to(ROOT)): digest(p) for p in active}}
parser = argparse.ArgumentParser()
parser.add_argument('--write', action='store_true', help='write verification.json in this evidence directory')
args = parser.parse_args()
if args.write:
    (HERE / 'verification.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({k: v for k, v in result.items() if k != 'current_document_hashes'}, ensure_ascii=False, indent=2))
raise SystemExit(0 if result['status'] == 'pass' else 1)
