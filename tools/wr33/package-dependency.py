"""Freeze the unchanged checkpointed Notification source; run on Fedora only."""
import hashlib
import json
from pathlib import Path
import shutil
import stat
import sys
import zipfile

run = Path(sys.argv[1]).resolve()
historical = Path.home() / "workspace/ani-iam-runs/wr24-fedora-20260915T113643Z"
manifest_path = historical / "final-candidate.json"
assert hashlib.sha256(manifest_path.read_bytes()).hexdigest() == "b514598675d75f446965169972e96144517d109173d453fdfe66dcb15b8c821b"
source = historical / "candidates/notification"
baseline = json.loads(manifest_path.read_text())["repositories"]["notification"]
assert len(baseline["files"]) == 105
snapshot = run / "inputs/notification-wr23"
destination = run / "artifacts/notification-r1"
snapshot.mkdir(exist_ok=False)
destination.mkdir(exist_ok=False)
module = "github.com/zhangzhe-ctrl/ani-notification-service"
version = "v0.0.1-wr33.1"
target = destination / "proxy" / module / "@v"
target.mkdir(parents=True)
for entry in baseline["files"]:
    relative = Path(entry["path"])
    assert not relative.is_absolute() and ".." not in relative.parts
    path = source / relative
    assert not path.is_symlink() and path.is_file()
    assert hashlib.sha256(path.read_bytes()).hexdigest() == entry["sha256"], relative
    assert oct(stat.S_IMODE(path.stat().st_mode)) == entry["mode"], relative
    (snapshot / relative).parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, snapshot / relative)
with zipfile.ZipFile(target / (version + ".zip"), "x", compression=zipfile.ZIP_DEFLATED) as archive:
    for entry in sorted(baseline["files"], key=lambda e:e["path"]):
        path = snapshot / entry["path"]
        record = zipfile.ZipInfo(module + "@" + version + "/" + entry["path"], (2026,9,16,0,0,0))
        record.external_attr = (stat.S_IFREG | stat.S_IMODE(path.stat().st_mode)) << 16
        archive.writestr(record, path.read_bytes())
(target / (version + ".mod")).write_bytes((snapshot / "go.mod").read_bytes())
(target / (version + ".info")).write_text(json.dumps({"Version":version,"Time":"2026-09-16T00:00:00Z"})+"\n")
(target / "list").write_text(version+"\n")
result = {"module":module,"version":version,"published":False,"source_head":baseline["head"],"source_manifest_sha256":hashlib.sha256(manifest_path.read_bytes()).hexdigest(),"entries":baseline["files"],"artifacts":{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(target.iterdir())}}
(destination / "manifest.json").write_text(json.dumps(result,indent=2,sort_keys=True)+"\n")
print(json.dumps({"manifest_sha256":hashlib.sha256((destination / "manifest.json").read_bytes()).hexdigest(),"unchanged_source_files":len(baseline["files"])},indent=2))
