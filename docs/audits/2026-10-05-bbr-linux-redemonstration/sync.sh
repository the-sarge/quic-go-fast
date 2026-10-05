#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Excluded paths are protected from --delete: observations, stage summaries, prerequisites and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-linux-redemonstration
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/prereq/" --exclude="$art/*.json" \
	--exclude="$art/*.log" --exclude="$art/*.pem" --exclude="$art/mac/" \
	"$root/" minimax:agents/bbr-linux-redemonstration/
