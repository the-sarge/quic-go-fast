#!/bin/sh
# Sync this record's worktree (sources and aids, without .git) and its built
# binaries to minimax (#777's sync.sh with this record's paths). The remote
# .local tree's observations and calibration are never touched: rsync does not
# delete excluded paths. With "src", also sync the exported build trees (without
# their docs) for compiling there with build.py compile.
set -eu
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
REMOTE=agents/bbr-l6-diagnosis
rsync -a --delete --exclude='.git' --exclude='/.local/' --exclude='__pycache__' --exclude='/RUNNING' --exclude='/STAGE.DONE' "$ROOT/" "minimax:$REMOTE/"
ssh minimax "mkdir -p $REMOTE/.local/bbr-l6-diagnosis/bin"
if [ "${1:-}" = src ]; then
	rsync -a --delete --exclude='/*/docs' "$ROOT/.local/bbr-l6-diagnosis/src/" "minimax:$REMOTE/.local/bbr-l6-diagnosis/src/"
fi
