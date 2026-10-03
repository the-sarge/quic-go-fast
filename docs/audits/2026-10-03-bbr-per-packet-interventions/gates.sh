#!/bin/sh
# Registered #714 gates for one intervention revision, run at the worktree's HEAD.
# Usage: gates.sh rK   (writes gates/rK.log next to this script; never overwrites)
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
log="$here/gates/$1.log"
mkdir -p "$here/gates"
[ -e "$log" ] && { echo "$log exists" >&2; exit 2; }
export GOWORK=off GOTOOLCHAIN=go1.27.0
cd "$root" || exit 2
root_subset='BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing'
status=0
step() {
	echo "\$ $*" >>"$log"
	"$@" >>"$log" 2>&1 || { status=1; echo "FAILED: $*" >>"$log"; }
}
{
	echo "# Gates for $1 at $(git rev-parse HEAD)"
	echo "# $(go version); tree clean: $(git status --porcelain -- ':!.local' ':!docs/audits/2026-10-03-bbr-per-packet-interventions' | wc -l | tr -d ' ') changed paths"
} >"$log"
step go vet ./...
step go test ./internal/ackhandler/... ./internal/congestion/... -count=1
step go test -race ./internal/ackhandler/ ./internal/congestion/ -count=1
step go test . -run "$root_subset" -count=1
step go test -race . -run "$root_subset" -count=1
# Full root package: tallied outcomes, with go test's own exit status.
step sh -c 'out=$(mktemp); go test . -count=1 -v >"$out" 2>&1; s=$?; grep -E "^(--- FAIL|FAIL|ok|panic)" "$out"; grep -cE "^--- PASS" "$out" | sed "s/$/ top-level PASS/"; grep -cE "^--- SKIP" "$out" | sed "s/$/ top-level SKIP/"; rm -f "$out"; exit $s'
echo "# status $status" >>"$log"
exit $status
