#!/bin/sh
# #740 gates for the ring treatment: #714's gate set (as #734 ran it, with Capability and
# Pacing in the root subset) in one exported gate tree built by build.py (cand-mem, cand-mem-ring, gate-ring-m1, gate-ring-m2).
# Usage: gates.sh cand-mem-ring   (writes gates/cand-mem-ring.log next to this script; never overwrites)
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
tree="$root/.local/bbr-r9-memory-attribution/src/$1"
log="$here/gates/$1.log"
[ -d "$tree" ] || { echo "$tree missing; run build.py $1" >&2; exit 2; }
[ -e "$log" ] && { echo "$log exists" >&2; exit 2; }
export GOWORK=off GOTOOLCHAIN=go1.27.0
cd "$tree" || exit 2
root_subset='BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing|Capability'
status=0
step() {
	echo "\$ $*" >>"$log"
	"$@" >>"$log" 2>&1 || { status=1; echo "FAILED: $*" >>"$log"; }
}
{
	echo "# Gates for $1 (exported tree; build receipt bin/$1-build.json)"
	echo "# $(go version)"
	grep -n 'maxRecoveryOutcomes = ' internal/ackhandler/bbr_recovery.go
} >"$log"
step go vet .
step go test ./internal/ackhandler/... ./internal/congestion/... -count=1
step go test -race ./internal/ackhandler/ ./internal/congestion/ -count=1
step go test -tags bbrworkcount ./internal/ackhandler/ -count=1
step go test . -run "$root_subset" -count=1
step go test -race . -run "$root_subset" -count=1
# Full root package: tallied outcomes, with go test's own exit status.
step sh -c 'out=$(mktemp); go test . -count=1 -v >"$out" 2>&1; s=$?; grep -E "^(--- FAIL|FAIL|ok|panic)" "$out"; grep -cE "^--- PASS" "$out" | sed "s/$/ top-level PASS/"; grep -cE "^--- SKIP" "$out" | sed "s/$/ top-level SKIP/"; rm -f "$out"; exit $s'
echo "# status $status" >>"$log"
exit $status
