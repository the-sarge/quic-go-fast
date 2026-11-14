#!/usr/bin/env bash
# B5 #1044: the clean rerun of the aggregation campaign after the first invocation
# stopped on a contaminated observation and its contaminated replacement. It waits
# for the shared host's CI runner and Windows VM to leave for five minutes, then runs
# the unchanged driver and binary once against the remaining ledger.
set -euo pipefail
campaign=$1 remaining_instances=$2 remaining_host_seconds=$3
out="$campaign/aggregation-rerun"
[ ! -e "$out" ] || { echo "output $out exists" >&2; exit 2; }
exec >> "$campaign/rerun.log" 2>&1
pattern='[R]unner.Worker|[t]ransfer.test|qemu.*guest=[i]nfra-garm|[n]ativerig'
quiet=0
while [ "$quiet" -lt 300 ]; do
	if pgrep -f "$pattern" > /dev/null; then quiet=0; else quiet=$((quiet + 15)); fi
	sleep 15
done
echo "$(date -u +%FT%TZ) host quiet for 300 s; starting"
status=0
python3 -B "$campaign/src/interop/multipath/aggregation.py" \
	--candidate 4e80d3cf4fa7454ffc3e2a46655afd5f81f86313 --binary "$campaign/aggregation.test" \
	--output "$out" --remaining-instances "$remaining_instances" \
	--remaining-host-seconds "$remaining_host_seconds" --timeout-seconds 120 || status=$?
echo "$(date -u +%FT%TZ) aggregation rerun exit $status"
