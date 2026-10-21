#!/bin/sh
# Sync this worktree to minimax without ever deleting retained measurement output.
# Copied from slice 6's sync.sh (9b38f0ab, itself #739's) with recorded changes: the paths, and one
# sync for all three #808 stages (readiness, the side stage and L6 share ~/agents/quic-808), so each
# stage's retained output and the markers are protected from --delete. SYNC_TARGET overrides the
# destination, for testing the exclusions against a local directory.
# Excluded paths are protected from --delete: observations, stage summaries, prerequisites and credentials.
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-808-readiness
gso=.local/bbr-808-gso
l6=.local/bbr-808-l6
# Every host-written path is excluded, so --delete never removes it and the Mac never overwrites it:
# readiness observations, prerequisites, development runs and top-level JSON and logs; the side
# stage's runs and development runs; L6's observations, calibration, host-fact snapshots, JSON and
# logs; credentials; the markers; and the side stage's yield request.
rsync -a --delete --exclude='.git' --exclude="$art/observations*/" --exclude="$art/prereq/" --exclude="$art/dev/" \
	--exclude="$art/*.json" --exclude="$art/*.log" --exclude="$art/*.pem" \
	--exclude="$gso/runs/" --exclude="$gso/dev/" --exclude="$gso/*.log" --exclude="$gso/*.pem" \
	--exclude="$l6/observations*/" --exclude="$l6/prereq*/" --exclude="$l6/hostfacts/" --exclude="$l6/*.json" \
	--exclude="$l6/*.log" --exclude="$l6/*.pem" \
	--exclude='/RUNNING' --exclude='/STAGE.DONE' --exclude='/L6.DONE' --exclude='/YIELD-808.request' \
	"$root/" "${SYNC_TARGET:-minimax:agents/quic-808/}"
