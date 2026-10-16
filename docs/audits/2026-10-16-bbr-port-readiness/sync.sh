#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Copied from #739's sync.sh (138223ac); only the two paths change.
# Excluded paths are protected from --delete: observations, stage summaries, prerequisites and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-port-readiness
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/prereq/" --exclude="$art/*.json" \
	--exclude="$art/*.log" --exclude="$art/*.pem" \
	"$root/" minimax:agents/bbr-port-readiness/
