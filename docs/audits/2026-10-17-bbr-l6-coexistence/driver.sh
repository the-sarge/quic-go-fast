#!/bin/sh
# Unattended #777 order on minimax (adapted from the #748 slice 6 readiness
# record's driver.sh): host facts, the deployed-binary check, the prerequisites,
# smoke, the counted stage, the #714 reruns, host facts. Every step runs under
# taskset -c 1. A binary, prerequisite, smoke or correctness failure stops the
# order; nothing else is built or tested on minimax while it runs.
# With --preflight-only it runs only the clean-start check (L6_ART overrides the
# artifact directory, for testing that check).
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
ART="${L6_ART:-$HERE/../../../.local/bbr-l6-coexistence}"
LOG="$ART/driver.log"
log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" >> "$LOG"; }
# run_case reuses an existing clean observation, so a leftover would count as this
# order's. Each path or glob is checked on its own; an unmatched glob stays literal.
for p in "$ART"/observations/main-* "$ART"/observations/rerun-* "$ART"/observations/smoke-* "$ART"/prereq \
	"$ART"/smoke.json "$ART"/follow-decisions.json "$ART"/correctness-failure.json "$ART"/binaries.json; do
	if [ -e "$p" ]; then
		log "PRESTART-NOT-EMPTY $p"
		exit 1
	fi
done
log PRESTART-EMPTY
[ "${1:-}" = --preflight-only ] && exit 0
cd "$HERE"
step() {
	log "START $*"
	if taskset -c 1 python3 matrix.py "$@" >> "$LOG" 2>&1; then
		log "DONE $*"
	else
		log "FAILED $* (exit $?)"
		taskset -c 1 python3 matrix.py hostfacts stop >> "$LOG" 2>&1
		exit 1
	fi
}
step hostfacts start
step binaries
step prereq
step smoke
step main
step follow
step hostfacts end
log ORDER-COMPLETE
