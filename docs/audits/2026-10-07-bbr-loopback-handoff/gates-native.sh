#!/bin/sh
# #738 native Linux gates for one exported gate tree, in #714's native form, on minimax under taskset -c 1-3.
# Usage (from the Mac, after sync.sh, never while a stage measures): gates-native.sh gate-4q
#   (writes gates/gate-4q-native.log; never overwrites)
set -u
here=$(cd "$(dirname "$0")" && pwd)
log="$here/gates/$1-native.log"
[ -e "$log" ] && { echo "$log exists" >&2; exit 2; }
dir="agents/bbr-loopback-handoff/.local/bbr-loopback-handoff/src/$1"
ssh minimax "cd $dir && export GOWORK=off GOTOOLCHAIN=go1.27.0 && {
echo '# Native Linux tests for $1 (exported tree) on minimax,' \$(uname -r)
go version
echo '\$ go test . -run \"BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing|Capability\" -count=1 -v (tallied)'
out=\$(mktemp); taskset -c 1-3 go test . -run 'BBR|Emission|Credit|Delivery|ECN|Continuation|Pacing|Capability' -count=1 -v >\$out 2>&1; s=\$?
grep -E '^(--- FAIL|FAIL|ok|panic)' \$out; echo \$(grep -cE '^--- PASS' \$out) top-level PASS, \$(grep -cE '^--- SKIP' \$out) top-level SKIP, exit \$s
echo '\$ go test -race . -run \"LocalSendCredit|BBRPendingCredit|BBRVariant|BBRCapability|PacingKick\" -count=1'
taskset -c 1-3 go test -race . -run 'LocalSendCredit|BBRPendingCredit|BBRVariant|BBRCapability|PacingKick' -count=1 2>&1 | tail -3
echo '\$ go test ./internal/ackhandler/ ./internal/congestion/ -count=1'
taskset -c 1-3 go test ./internal/ackhandler/ ./internal/congestion/ -count=1 2>&1 | tail -3
}" > "$log" 2>&1
cat "$log"
