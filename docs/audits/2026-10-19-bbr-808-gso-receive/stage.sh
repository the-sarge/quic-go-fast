#!/bin/sh
# #808's registered side-stage order on minimax, run by the readiness driver after its last step
# (README, "Order"): eight counted rounds of every arm, one same-round rerun of each contaminated
# round, then four S-pre/S-rev pairs of #743's perf diagnostic. Sequential, under taskset -c 1-3.
set -eu
cd ~/agents/quic-808/docs/audits/2026-10-19-bbr-808-gso-receive
T="taskset -c 1-3"
O=../../../.local/bbr-808-gso/runs
mkdir -p "$O"
for p in "$O"/counted.jsonl "$O"/rerun.jsonl "$O"/perfdiag.jsonl; do
	if [ -e "$p" ]; then echo "SIDE-PRESTART-NOT-EMPTY $p"; exit 1; fi
done
echo "SIDE-START $(date -u +%FT%TZ)"
: > "$O"/rerun.jsonl  # empty unless a contaminated round is rerun, so the analysis command always has it
$T python3 runner.py "$O" 8 --tag=counted --duration=10
for n in $($T python3 analyze.py --contaminated "$O"/counted.jsonl); do
	$T python3 runner.py "$O" 1 --tag=rerun --first="$n" --duration=10
done
$T python3 perfdiag.py "$O" 4 --duration=10
echo "SIDE-END $(date -u +%FT%TZ)"
