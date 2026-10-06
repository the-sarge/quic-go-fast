#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Excluded paths are protected from --delete: observations, stage summaries, host facts and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-per-packet-cpu-leads
rsync -a --delete --exclude='.git' --exclude="$art/observations/" --exclude="$art/*.json" --exclude="$art/*.pem" \
	"$root/" minimax:agents/bbr-per-packet-cpu-leads/
