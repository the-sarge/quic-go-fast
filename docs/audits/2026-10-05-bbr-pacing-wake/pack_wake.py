#!/usr/bin/env python3
"""Pack #734's retained raw data into raw.tar.gz with a hashed manifest.

Adapted from #715's pack.py (copied byte-identical beside this file). Every
observation (including excluded smoke runs), synthetic and bare-timer run,
stage log and build receipt is packed. Binaries, credentials, exported source
trees and root-owned perf.data files are omitted, as in #715. New here: the
Go execution traces (*.trace) and the recorder's binary event files
(*.events) are too large for the repository; they are listed in the manifest
with their sizes and SHA-256 hashes and retained privately (README, "Assets").
Their analysis outputs (*.ana.json) and the recorder's meta (*.json) are packed.
"""
import hashlib
import io
import json
import tarfile
from pathlib import Path

import art  # noqa: F401
from run import ART

HERE = Path(__file__).resolve().parent
SKIP_SUFFIX = ('.perf.data', '.pem', '-fixture')
LISTED_SUFFIX = ('.trace', '.events')


def keep(p):
    rel = p.relative_to(ART)
    if rel.parts[0] in ('src', 'superseded') or p.name.endswith(SKIP_SUFFIX):
        return False
    if rel.parts[0] == 'bin':
        return p.name.endswith('-build.json') or p.name == 'oracle.json'
    return p.is_file()


files = sorted(p for p in ART.rglob('*') if p.is_file() and keep(p))
entries, listed = [], []
buf = io.BytesIO()
with tarfile.open(fileobj=buf, mode='w:gz', compresslevel=9) as tar:
    for p in files:
        data = p.read_bytes()
        rel = str(p.relative_to(ART))
        e = dict(path=rel, bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
        if p.name.endswith(LISTED_SUFFIX):
            listed.append(e)
            continue
        entries.append(e)
        info = tarfile.TarInfo(rel)
        info.size, info.mtime, info.mode = len(data), 0, 0o644
        tar.addfile(info, io.BytesIO(data))
(HERE / 'raw.tar.gz').write_bytes(buf.getvalue())
manifest = dict(archive='raw.tar.gz', archive_sha256=hashlib.sha256(buf.getvalue()).hexdigest(),
                note=__doc__.split('\n\n')[1].replace('\n', ' '), files=len(entries), entries=entries,
                retained_privately=dict(files=len(listed), bytes=sum(e['bytes'] for e in listed), entries=listed),
                builds=sorted(e['path'].split('/')[-1] for e in entries if e['path'].startswith('bin/')))
(HERE / 'raw-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print(manifest['files'], 'files', len(buf.getvalue()) // 2**20, 'MiB;', len(listed), 'listed', manifest['retained_privately']['bytes'] // 2**20, 'MiB')
