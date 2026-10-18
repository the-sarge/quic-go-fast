#!/usr/bin/env python3
"""Pack this record's raw data (#792) into raw.tar.gz with raw-manifest.json.

#777's pack.py (71e88789) with this record's paths: its artifact directory
(.local/bbr-l6-diagnosis) and its own record directory for the outputs; the
superseded build receipts in old/ are kept and its binaries skipped, as in bin/.

Every observation (counted, smoke and development, with each trace),
calibration, driver.log, follow-decisions.json, smoke.json, binaries.json, the
hostfacts snapshots, the development logs and the build receipts. Binaries,
credentials and exported source trees are omitted. The archive is
deterministic: sorted entries, fixed owner and modification time.
"""
import gzip
import hashlib
import io
import json
import tarfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ART = HERE.parents[2] / '.local/bbr-l6-diagnosis'
SKIP_DIRS = {'src'}
SKIP_FILES = {'campaign.pem', 'campaign-key.pem'}


def members():
    for p in sorted(ART.rglob('*')):
        rel = p.relative_to(ART)
        if not p.is_file() or rel.parts[0] in SKIP_DIRS or p.name in SKIP_FILES:
            continue
        if rel.parts[0] in ('bin', 'old') and not p.name.endswith('.json'):
            continue
        yield rel, p


def main():
    entries, buf = [], io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode='wb', mtime=0) as gz, tarfile.open(fileobj=gz, mode='w', format=tarfile.PAX_FORMAT) as tar:
        for rel, p in members():
            data = p.read_bytes()
            info = tarfile.TarInfo(str(rel))
            info.size, info.mtime, info.mode, info.uid, info.gid, info.uname, info.gname = len(data), 0, 0o644, 0, 0, '', ''
            tar.addfile(info, io.BytesIO(data))
            entries.append(dict(path=str(rel), bytes=len(data), sha256=hashlib.sha256(data).hexdigest()))
    (HERE / 'raw.tar.gz').write_bytes(buf.getvalue())
    manifest = dict(archive='raw.tar.gz', archive_sha256=hashlib.sha256(buf.getvalue()).hexdigest(),
                    note=__doc__.split('\n\n')[2].replace('\n', ' '), files=len(entries), entries=entries)
    (HERE / 'raw-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(len(entries), 'files', len(buf.getvalue()), 'bytes')


if __name__ == '__main__':
    main()
