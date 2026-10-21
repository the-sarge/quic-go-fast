#!/bin/sh
# Unattended #808 L6 order on minimax: #792's driver.sh (89e71a60, itself #777's with markers) with
# this record's paths and its own done marker, L6.DONE, because the readiness order already wrote
# STAGE.DONE in the same worktree; the clean-start check also covers this record's hostfacts/ directory.
# RUNNING is held for the whole order, and STAGE.DONE (the agreed "free" marker) is moved aside at the
# start and written again at the end, so the shared markers stay true during L6.
# #792's header follows.
# Unattended #792 order on minimax (#777's driver.sh with this record's paths):
# host facts, the deployed-binary check, the prerequisites, smoke, the counted
# stage, the #714 reruns, host facts. Every step runs under taskset -c 1. A
# binary, prerequisite, smoke or correctness failure stops the order; nothing
# else is built or tested on minimax while it runs. When the order ends, the
# file STAGE.DONE at the worktree root says how (the marker agreed with the
# wiremux sessions that share minimax; matrix.py keeps RUNNING while an
# observation runs).
# With --preflight-only it runs only the clean-start check (L6_ART overrides the
# artifact directory, for testing that check).
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
ART="${L6_ART:-$ROOT/.local/bbr-808-l6}"
LOG="$ART/driver.log"
log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" >> "$LOG"; }
# run_case reuses an existing clean observation, so a leftover would count as this
# order's. Each path or glob is checked on its own; an unmatched glob stays literal.
for p in "$ART"/observations/main-* "$ART"/observations/rerun-* "$ART"/observations/smoke-* "$ART"/prereq \
	"$ART"/smoke.json "$ART"/follow-decisions.json "$ART"/correctness-failure.json "$ART"/binaries.json "$ART"/hostfacts "$ROOT"/L6.DONE; do
	if [ -e "$p" ]; then
		log "PRESTART-NOT-EMPTY $p"
		exit 1
	fi
done
log PRESTART-EMPTY
[ "${1:-}" = --preflight-only ] && exit 0
# The wiremux sessions read RUNNING (busy) and STAGE.DONE (free) at the worktree root. The readiness
# order's STAGE.DONE is kept with this record's artifacts, RUNNING is held for the whole order, and on
# any exit RUNNING goes, L6.DONE says how the order ended ("stopped" if it ended early, signals
# included), and STAGE.DONE is written again with the same text.
mkdir -p "$ART"
[ -e "$ROOT"/STAGE.DONE ] && mv "$ROOT"/STAGE.DONE "$ART"/readiness-STAGE.DONE
touch "$ROOT"/RUNNING
finish() {
	[ -e "$ROOT"/L6.DONE ] || echo "stopped $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$ROOT"/L6.DONE
	cp "$ROOT"/L6.DONE "$ROOT"/STAGE.DONE
	rm -f "$ROOT"/RUNNING
}
trap finish EXIT
trap 'exit 1' HUP INT TERM
cd "$HERE"
step() {
	log "START $*"
	if taskset -c 1 python3 matrix.py "$@" >> "$LOG" 2>&1; then
		log "DONE $*"
	else
		log "FAILED $* (exit $?)"
		taskset -c 1 python3 matrix.py hostfacts stop >> "$LOG" 2>&1
		echo "stopped at $* $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$ROOT/L6.DONE"
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
echo "complete $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$ROOT/L6.DONE"
