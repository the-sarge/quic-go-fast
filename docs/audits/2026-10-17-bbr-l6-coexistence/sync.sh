#!/bin/sh
# Sync this record's worktree (sources and aids, without .git) to minimax. The
# remote .local tree (binaries, observations, calibration) is never touched:
# rsync does not delete excluded paths. With "aidtest", also sync the assembled
# aid-test module from build.py aidtest.
set -eu
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
REMOTE=agents/bbr-l6-coexistence
rsync -a --delete --exclude='.git' --exclude='/.local/' "$ROOT/" "minimax:$REMOTE/"
if [ "${1:-}" = aidtest ]; then
	ssh minimax "mkdir -p $REMOTE/.local/bbr-l6-coexistence/src"
	rsync -a --delete "$ROOT/.local/bbr-l6-coexistence/src/aidtest/" "minimax:$REMOTE/.local/bbr-l6-coexistence/src/aidtest/"
fi
