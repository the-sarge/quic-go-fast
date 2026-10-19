#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Copied from slice 6's sync.sh (9b38f0ab, itself #739's) with recorded changes: the paths, and one
# sync for all three #808 stages (readiness, the side stage and L6 share ~/agents/quic-808), so each
# stage's retained output and the RUNNING/STAGE.DONE markers are protected from --delete.
# Excluded paths are protected from --delete: observations, stage summaries, prerequisites and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-808-readiness
gso=.local/bbr-808-gso
l6=.local/bbr-808-l6
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/prereq/" --exclude="$art/*.json" \
	--exclude="$art/*.log" --exclude="$art/*.pem" \
	--exclude="$gso/runs/" --exclude="$gso/*.log" --exclude="$gso/*.pem" \
	--exclude="$l6/observations*/" --exclude="$l6/prereq*/" --exclude="$l6/*.json" --exclude="$l6/*.log" --exclude="$l6/*.pem" \
	--exclude='/RUNNING' --exclude='/STAGE.DONE' --exclude='/L6.DONE' \
	"$root/" minimax:agents/quic-808/
