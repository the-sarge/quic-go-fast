#!/usr/bin/env python3
"""#714 mutation sweep: does each new oracle reject a semantic defect in its change?

Each mutant replaces one exact source fragment (it must occur once), runs the named
oracles on a scratch copy, restores the file, and records killed or survived.
Usage: mutate.py TREE > mutation-results.tsv   (TREE holds the revision under test)
"""
import os
import subprocess
import sys
from pathlib import Path

ENV = {**os.environ, 'GOWORK': 'off', 'GOTOOLCHAIN': 'go1.27.0'}
REC = ('./internal/ackhandler/', 'TestDeliveryRecords|TestRecoveryEquivalence|TestRecoveryLowerBound|TestRecoveryBoundaryWitness')
CREDIT = ('.', 'TestLocalSendCredit|TestBBRPendingCredit')
MUTANTS = [
    # lead 2: delivery records
    ('D1', 'internal/ackhandler/delivery_records.go', '\t\tif (i < j && i < h && h <= j) || (j < i && (i < h || h <= j)) {\n\t\t\tcontinue\n\t\t}',
     '\t\tif (i < j && i < h && h <= j) || (j < i && i < h) {\n\t\t\tcontinue\n\t\t}', 'backward shift ignores wrapped homes', REC),
    ('D2', 'internal/ackhandler/delivery_records.go', '\tfor j := (i + 1) & mask; t.slots[j].key != 0; j = (j + 1) & mask {',
     '\tfor j := (i + 1) & mask; false && t.slots[j].key != 0; j = (j + 1) & mask {', 'deletion leaves a hole in the probe run', REC),
    ('D3', 'internal/ackhandler/delivery_records.go', 'return uint64(k.number)<<2 | uint64(recoverySpace(k.space)+1)',
     'return uint64(k.number)<<2 | 1', 'key drops the space', REC),
    ('D4', 'internal/ackhandler/delivery_records.go', '\tt.recs[r] = deliveryRecord{}\n\tt.free = append(t.free, r)',
     '\tt.free = append(t.free, r)', 'deleted record stays visible to iteration', REC),
    ('D5', 'internal/ackhandler/delivery_records.go', '\tif t.n == 0 {\n\t\treturn -1\n\t}\n\tmask := len(t.slots) - 1\n\tfor i := t.home(key); ; i = (i + 1) & mask {',
     '\tif t.n == 0 {\n\t\treturn -1\n\t}\n\tmask := len(t.slots) - 1\n\tfor i := t.home(key); ; i = (i + 2) & mask {', 'probe skips slots', REC),
    # lead 3: recovery-evidence bracket
    ('R1', 'internal/ackhandler/bbr_recovery.go', 'lo, hi := int(max(0, d-q.gaps)), int(min(int64(q.n), d))',
     'lo, hi := int(max(0, d-q.gaps+1)), int(min(int64(q.n), d))', 'bracket low end one too high', REC),
    ('R2', 'internal/ackhandler/bbr_recovery.go', 'lo, hi := int(max(0, d-q.gaps)), int(min(int64(q.n), d))',
     'lo, hi := int(max(0, d-q.gaps)), int(min(int64(q.n), d-1))', 'bracket high end one too low', REC),
    ('R3', 'internal/ackhandler/recovery_index.go', '\tnext := outcomes[q.at(0)].number\n\tq.gaps -= int64(next - q.first - 1)\n',
     '\tnext := outcomes[q.at(0)].number\n', 'eviction keeps the evicted gap', REC),
    ('R4', 'internal/ackhandler/bbr_recovery.go', '\treturn w.number[s], w.noted[s]\n', '\treturn w.number[s], true\n', 'absent witness reported present', REC),
    ('R5', 'internal/ackhandler/bbr_recovery.go', '\tif pn > q.last {\n\t\treturn q.n\n\t}', '\tif pn > q.last+1 {\n\t\treturn q.n\n\t}', 'above-last shortcut off by one', REC),
    # lead 5: credit recycling and connection-side signals
    ('C1', 'local_send_credit.go', 'func (r *sendReservation) complete() { r.release(0, true) }',
     'func (r *sendReservation) complete() { r.release(0, false) }', 'worker completion does not signal', CREDIT),
    ('C2', 'local_send_credit.go', '\tif n == 0 {\n\t\t*r = sendReservation{}\n\t\tc.free = append(c.free, r)\n\t}',
     '\tif n <= r.bytes {\n\t\tc.free = append(c.free, r)\n\t}', 'reservation recycled while still live', CREDIT),
    ('C3', 'local_send_credit.go', 'func (r *sendReservation) completeLocal() { r.release(0, false) }',
     'func (r *sendReservation) completeLocal() { r.release(0, true) }', 'connection-side release signals (the predecessor behaviour)', CREDIT),
    ('C4', 'local_send_credit.go', '\t*r = sendReservation{owner: c, bytes: n, generation: c.generation, isolated: isolated}',
     '\t*r = sendReservation{owner: c, bytes: n, generation: c.generation, isolated: isolated || r.isolated}', 'reused reservation keeps stale isolation', CREDIT),
]


def main(tree):
    tree = Path(tree)
    print('id\tfile\tdescription\tresult')
    for mid, rel, old, new, desc, (pkg, run) in MUTANTS:
        path = tree / rel
        src = path.read_text()
        assert src.count(old) == 1, (mid, rel)
        path.write_text(src.replace(old, new))
        try:
            r = subprocess.run(['go', 'test', pkg, '-run', run, '-count=1', '-short'], cwd=tree, env=ENV, capture_output=True, text=True, timeout=900)
            result = 'survived' if r.returncode == 0 else ('killed' if 'FAIL' in r.stdout + r.stderr else f'error {r.returncode}')
        finally:
            path.write_text(src)
        print(f'{mid}\t{rel}\t{desc}\t{result}', flush=True)


if __name__ == '__main__':
    main(sys.argv[1])
