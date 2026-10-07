#!/bin/sh
# #738 gates for the diagnostic pending-bound arm: #714's gate set (as #734 ran it, with Capability and
# Pacing in the root subset) in one exported gate tree built by build.py (gate-r8, gate-4q, gate-m1).
# Usage: gates.sh gate-4q   (writes gates/gate-4q.log next to this script; never overwrites)
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
tree="$root/.local/bbr-loopback-handoff/src/$1"
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
	grep -n 'quantum, ordinary, isolated)\|quantum, size, ordinary, isolated)' packet_emission_bbr.go
	echo "# variant bound: $(grep variantBoundQ zz_variant_bound_test.go)"
} >"$log"
step go vet .
step go test ./internal/ackhandler/... ./internal/congestion/... -count=1
step go test -race ./internal/ackhandler/ ./internal/congestion/ -count=1
step go test -tags bbrworkcount ./internal/ackhandler/ -count=1
step go test . -run 'BBRVariant' -count=1 -v
step go test . -run "$root_subset" -count=1
step go test -race . -run "$root_subset" -count=1
# Full root package: tallied outcomes, with go test's own exit status.
step sh -c 'out=$(mktemp); go test . -count=1 -v >"$out" 2>&1; s=$?; grep -E "^(--- FAIL|FAIL|ok|panic)" "$out"; grep -cE "^--- PASS" "$out" | sed "s/$/ top-level PASS/"; grep -cE "^--- SKIP" "$out" | sed "s/$/ top-level SKIP/"; rm -f "$out"; exit $s'
echo "# status $status" >>"$log"
exit $status
