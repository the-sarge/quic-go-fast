#!/usr/bin/env python3
"""Pack #712's retained raw data into raw.tar.gz with a hashed manifest.

Every observation (including excluded smoke runs), prerequisites, stage logs
and build receipts. Binaries, credentials (campaign*.pem), exported source
trees and root-owned perf.data files are omitted; their folded call stacks are
kept.
"""
import hashlib
import io
import json
import tarfile
from pathlib import Path

from run import ART

HERE = Path(__file__).resolve().parent
SKIP_SUFFIX = ('.perf.data', '.pem', '-fixture')


def keep(p):
    rel = p.relative_to(ART)
    if rel.parts[0] in ('src',) or p.name.endswith(SKIP_SUFFIX):
        return False
    if rel.parts[0] == 'bin':
        return p.name.endswith('-build.json')
    return p.is_file()


files = sorted(p for p in ART.rglob('*') if p.is_file() and keep(p))
entries = []
buf = io.BytesIO()
with tarfile.open(fileobj=buf, mode='w:gz', compresslevel=9) as tar:
    for p in files:
        data = p.read_bytes()
        rel = str(p.relative_to(ART))
        entries.append(dict(path=rel, bytes=len(data), sha256=hashlib.sha256(data).hexdigest()))
        info = tarfile.TarInfo(rel)
        info.size, info.mtime, info.mode = len(data), 0, 0o644
        tar.addfile(info, io.BytesIO(data))
(HERE / 'raw.tar.gz').write_bytes(buf.getvalue())
manifest = dict(archive='raw.tar.gz', archive_sha256=hashlib.sha256(buf.getvalue()).hexdigest(),
                note=pack_note if (pack_note := __doc__.split('\n\n')[1].replace('\n', ' ')) else '',
                files=len(entries), entries=entries,
                builds=sorted(e['path'].split('/')[-1] for e in entries if e['path'].startswith('bin/')))
(HERE / 'raw-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print(manifest['files'], 'files', len(buf.getvalue()) // 2**20, 'MiB')
