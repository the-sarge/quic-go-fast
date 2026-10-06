#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Excluded paths are protected from --delete: observations, synthetic runs, stage summaries, logs and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-pacing-wake
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/synthetic*/" --exclude="$art/bare*/" \
	--exclude="$art/*.json" --exclude="$art/*.log" --exclude="$art/*.pem" \
	"$root/" minimax:agents/bbr-pacing-wake/
