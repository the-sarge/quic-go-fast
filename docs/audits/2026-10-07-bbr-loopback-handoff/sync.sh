#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Excluded paths are protected from --delete: observations, stage summaries, logs, credentials and gate trees' results.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-loopback-handoff
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/*.json" --exclude="$art/*.log" \
	--exclude="$art/*.pem" --exclude="$art/folded/" \
	"$root/" minimax:agents/bbr-loopback-handoff/
