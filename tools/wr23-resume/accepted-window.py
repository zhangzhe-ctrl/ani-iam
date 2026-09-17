#!/usr/bin/env python3
"""Verify an immutable prefix of the actual WR23 observation; never claim 24h."""
import argparse
import datetime as dt
import hashlib
import json
import math
from pathlib import Path


def stamp(value):
    return dt.datetime.fromisoformat(value.replace('Z', '+00:00'))


def verify(state, rows):
    assert state['kind'] in ('active24h', 'accepted12h30')
    assert state['result'] in ('running', 'pass')
    assert state['full_rebuild'] == 'pass'
    count = state['samples']
    assert count == len(rows) and count >= 3000
    assert state['authorization_comparisons'] == 2 * count
    assert rows[-1]['observed_at'] == state['last_observed_at']
    start, finish = stamp(state['started_at']), stamp(rows[-1]['observed_at'])
    elapsed = (finish - start).total_seconds()
    post = (finish - stamp(state['rebuild_activated_at'])).total_seconds()
    assert elapsed >= 45000 and post >= 1800
    assert stamp(state['rebuild_activated_at']) >= start
    assert len({r['event_id'] for r in rows}) == count
    previous = None
    latencies, gaps = [], []
    for index, row in enumerate(rows, 1):
        assert row['index'] == index and row['tenant_id'] == state['tenant_id']
        assert row['unexplained_difference'] is False
        assert row['unresolved_tenant_gaps'] == row['quarantined_events'] == 0
        assert row['owner_status'] == ('frozen' if index % 2 else 'active')
        assert row['membership_read_status'] == row['expected_membership_read_status'] == (403 if index % 2 else 200)
        assert row['role_read_status'] == 403
        assert len(row['raw_sha256']) == 64
        origin, observed = stamp(row['occurred_at']), stamp(row['observed_at'])
        latency = row['propagation_seconds']
        assert origin >= start and observed <= finish and 0 <= latency <= 30
        assert abs((observed-origin).total_seconds()-latency) < .001
        if previous is not None:
            gap = (origin-stamp(previous['occurred_at'])).total_seconds()
            assert 0 < gap <= 45
            assert row['source_sequence'] > previous['source_sequence']
            assert row['lifecycle_version'] == previous['lifecycle_version'] + 1
            gaps.append(gap)
        latencies.append(latency)
        previous = row
    assert (stamp(rows[0]['occurred_at'])-start).total_seconds() <= 45
    assert state['unexplained_authorization_differences'] == state['unresolved_tenant_gaps'] == state['quarantined_events'] == 0
    latencies.sort()
    p99 = latencies[math.ceil(.99 * count)-1]
    assert p99 <= 5
    return dict(result='pass', kind='accepted12h30', active_24h='not_verified',
                started_at=state['started_at'], finished_at=rows[-1]['observed_at'],
                elapsed_seconds=elapsed, post_rebuild_seconds=post,
                rebuild_activated_at=state['rebuild_activated_at'], full_rebuild='pass',
                samples=count, authorization_comparisons=2*count,
                p99_seconds=p99, max_propagation_seconds=max(latencies),
                max_activity_gap_seconds=max(gaps), unresolved_tenant_gaps=0,
                unexplained_authorization_differences=0, quarantined_events=0,
                frozen_inputs_sha256=state['frozen_inputs_sha256'])


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('run', type=Path)
    p.add_argument('--authorization', type=Path, required=True)
    p.add_argument('--authorization-sha256', required=True)
    args = p.parse_args()
    authorization = args.authorization.read_bytes()
    assert hashlib.sha256(authorization).hexdigest() == args.authorization_sha256
    decision = json.loads(authorization)
    assert decision['minimum_elapsed_seconds'] == 45000 and decision['minimum_post_rebuild_seconds'] == 1800
    assert decision['observation_run'] == args.run.name
    raw = (args.run/'shadow-state-results.json').read_bytes()
    state = json.loads(raw)
    # State is committed only after the corresponding sample fsync. Ignore any
    # later appended rows; the acceptance snapshot is a complete durable prefix.
    lines = (args.run/'shadow-samples.jsonl').read_bytes().splitlines(keepends=True)[:state['samples']]
    assert len(lines) == state['samples'] and all(x.endswith(b'\n') for x in lines)
    sample_raw = b''.join(lines)
    proof = verify(state, [json.loads(x) for x in lines])
    if state['result'] == 'running':
        executable = (Path('/proc')/str(int(state['pid']))/'exe').resolve(strict=True)
        assert executable.is_relative_to(args.run/'private')
    proof.update(observation_run=args.run.name,
                 observation_manifest_sha256=decision['observation_manifest_sha256'],
                 authorization_sha256=args.authorization_sha256,
                 source_state_sha256=hashlib.sha256(raw).hexdigest(),
                 samples_sha256=hashlib.sha256(sample_raw).hexdigest(),
                 verifier_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                 original_process_state=state['result'])
    for name, content in [('accepted-window-source-state-results.json', raw),
                          ('accepted-window-samples.jsonl', sample_raw),
                          ('accepted-window-results.json', (json.dumps(proof, indent=2)+'\n').encode())]:
        with (args.run/name).open('xb') as f:
            f.write(content)
            f.flush()
            import os
            os.fsync(f.fileno())
    print(json.dumps(proof))


if __name__ == '__main__':
    main()
