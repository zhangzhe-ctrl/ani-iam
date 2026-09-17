"""Frozen WR23 execution host; historical evidence keeps its original host."""
import json
import os
import pwd
from pathlib import Path


def environment(record=None):
    if record is None:
        result = json.loads(Path(__file__).with_name("remote-environment.json").read_text())
    else:
        result = record.get("environment", {"ssh_host": "ubuntu", "user": "ubuntu", "hostname": "i-8yg2l7u8", "run_root": "/home/ubuntu/workspace/ani-iam-runs"})
    assert result["run_root"] == "/home/ubuntu/workspace/ani-iam-runs"
    assert (result["ssh_host"], result["user"], result["hostname"]) in {
        ("ani-test-1", "ubuntu", "ani-01"), ("ubuntu", "ubuntu", "i-8yg2l7u8")
    }
    return result


def run_environment(evidence, run):
    record = json.loads((evidence / "runs" / run / "run.json").read_text())
    assert record["run_id"] == run
    result = environment(record)
    assert record["remote"] == result["run_root"] + "/" + run
    return result


def verify_runtime(run):
    """Check the native owner tool against its frozen run before any mutation."""
    record = json.loads((run / "run.json").read_text())
    result = environment(record)
    assert record["run_id"] == run.name
    assert record["remote"] == str(run) == result["run_root"] + "/" + run.name
    assert os.uname().nodename == result["hostname"]
    assert pwd.getpwuid(os.geteuid()).pw_name == result["user"]
    return result
