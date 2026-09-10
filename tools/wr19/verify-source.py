#!/usr/bin/env python3
"""Verify an explicit, non-secret WR-19 source snapshot before execution."""
import hashlib
import json
from pathlib import Path
import sys

manifest_path, source_path = map(Path, sys.argv[1:])
manifest = json.loads(manifest_path.read_text())
names = set()
for row in manifest['files']:
    name = Path(row['path'])
    assert not name.is_absolute() and '..' not in name.parts
    assert str(name) not in names
    names.add(str(name))
    path = source_path / name
    assert path.is_file() and not path.is_symlink(), str(name)
    assert hashlib.sha256(path.read_bytes()).hexdigest() == row['sha256'], str(name)
    assert oct(path.stat().st_mode & 0o777) == row['mode'], str(name)
actual = {str(p.relative_to(source_path)) for p in source_path.rglob('*') if p.is_file() or p.is_symlink()}
assert actual == names, sorted(actual.symmetric_difference(names))
print(json.dumps({'source_verification': 'pass', 'files': len(names), 'archive_sha256': manifest['archive_sha256']}))
