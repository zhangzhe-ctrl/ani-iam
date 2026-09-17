"""Create an immutable file GOPROXY from this candidate, only on Fedora."""
import hashlib
import json
from pathlib import Path
import stat
import sys
import zipfile

source, destination = (Path(p).resolve() for p in sys.argv[1:3])
version = sys.argv[3]
if version not in ("v0.0.1-wr33.1", "v0.0.1-wr33.2", "v0.0.1-wr33.3", "v0.0.1-wr33.4"):
    raise RuntimeError("unreviewed candidate version")
destination.mkdir(parents=True, exist_ok=False)
manifest = {"version": version, "published": False, "modules": {}}
for name in ("api", "sdk", "workloadregistry"):
    root = source / name
    module = "github.com/zhangzhe-ctrl/ani-iam/" + name
    files = sorted(p for p in root.rglob("*") if p.is_file() and not any(x.startswith(".") for x in p.relative_to(root).parts))
    target = destination / "proxy" / module / "@v"
    target.mkdir(parents=True)
    entries = []
    with zipfile.ZipFile(target / (version + ".zip"), "x", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in files:
            if path.is_symlink():
                raise RuntimeError("module contains a symlink")
            relative = path.relative_to(root).as_posix()
            raw = path.read_bytes()
            mode = stat.S_IMODE(path.stat().st_mode)
            record = zipfile.ZipInfo(module + "@" + version + "/" + relative, (2026, 9, 16, 0, 0, 0))
            record.external_attr = (stat.S_IFREG | mode) << 16
            archive.writestr(record, raw)
            entries.append({"path": relative, "mode": oct(mode), "sha256": hashlib.sha256(raw).hexdigest()})
    (target / (version + ".mod")).write_bytes((root / "go.mod").read_bytes())
    (target / (version + ".info")).write_text(json.dumps({"Version": version, "Time": "2026-09-16T00:00:00Z"}) + "\n")
    (target / "list").write_text(version + "\n")
    manifest["modules"][module] = {"files": entries, "artifacts": {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(target.iterdir())}}
(destination / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
print(json.dumps({"version": version, "manifest_sha256": hashlib.sha256((destination / "manifest.json").read_bytes()).hexdigest(), "module_files": {k: len(v["files"]) for k,v in manifest["modules"].items()}}, indent=2))
