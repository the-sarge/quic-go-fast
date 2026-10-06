#!/bin/sh
# #735 native Linux gates for one revision (#734's, plus the full root package natively), on minimax under taskset -c 1-3, only between stages.
# Extra -race test patterns for the lead go in the optional third argument.
# Usage (from the Mac): gates-native.sh lK REV ['Pattern|...']   (exports REV, runs on minimax, writes gates/rK-native.log; never overwrites)
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
log="$here/gates/$1-native.log"
[ -e "$log" ] && { echo "$log exists" >&2; exit 2; }
rev=$(git -C "$root" rev-parse "$2")
dir="agents/bbr-cpu-leads-gates-$1"
extra=${3:+|$3}
ssh minimax "rm -rf $dir && mkdir -p $dir"
git -C "$root" archive --format=tar "$rev" | ssh minimax "tar -x -C $dir"
ssh minimax "cd $dir && export GOWORK=off GOTOOLCHAIN=go1.27.0 && {
echo '# Native Linux tests for $1 (code tree $rev, exported) on minimax,' \$(uname -r)
go version
echo '\$ go test . -run \"BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing|Capability\" -count=1 -v (tallied)'
out=\$(mktemp); taskset -c 1-3 go test . -run 'BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing|Capability' -count=1 -v >\$out 2>&1; s=\$?
grep -E '^(--- FAIL|FAIL|ok|panic)' \$out; echo \$(grep -cE '^--- PASS' \$out) top-level PASS, \$(grep -cE '^--- SKIP' \$out) top-level SKIP, exit \$s
echo '\$ go test -race . -run \"LocalSendCredit|BBRPendingCredit|BBRCapability|PacingKick$extra\" -count=1'
taskset -c 1-3 go test -race . -run 'LocalSendCredit|BBRPendingCredit|BBRCapability|PacingKick$extra' -count=1 2>&1 | tail -3
echo '\$ go test ./internal/ackhandler/ ./internal/congestion/ -count=1'
taskset -c 1-3 go test ./internal/ackhandler/ ./internal/congestion/ -count=1 2>&1 | tail -3
echo '\$ go test . -count=1 -v (full root package, tallied)'
out=\$(mktemp); taskset -c 1-3 go test . -count=1 -v >\$out 2>&1; s=\$?
grep -E '^(--- FAIL|FAIL|ok|panic)' \$out; echo \$(grep -cE '^--- PASS' \$out) top-level PASS, \$(grep -cE '^--- SKIP' \$out) top-level SKIP, exit \$s
}" > "$log" 2>&1
cat "$log"
