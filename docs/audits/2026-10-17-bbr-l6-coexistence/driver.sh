#!/bin/sh
# Unattended #777 order on minimax (adapted from the #748 slice 6 readiness
# record's driver.sh): host facts, the prerequisites, smoke, the counted stage,
# the #714 reruns, host facts. Every step runs under taskset -c 1. A prerequisite,
# smoke or correctness failure stops the order; nothing else is built or tested
# on minimax while it runs.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
ART="$HERE/../../../.local/bbr-l6-coexistence"
LOG="$ART/driver.log"
cd "$HERE"
log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" >> "$LOG"; }
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
# run_case reuses an existing clean observation, so leftovers would count as this order's.
if ls -d "$ART"/observations/main-* "$ART"/observations/rerun-* "$ART"/observations/smoke-* "$ART"/prereq \
	"$ART"/smoke.json "$ART"/follow-decisions.json >/dev/null 2>&1; then
	log PRESTART-NOT-EMPTY
	exit 1
fi
log PRESTART-EMPTY
step hostfacts start
step prereq
step smoke
step main
step follow
step hostfacts end
log ORDER-COMPLETE
