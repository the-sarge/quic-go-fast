#!/bin/sh
# Copy retained measurement output back from minimax (observations, stage logs and summaries); never deletes locally.
# perf.data written by sudo perf is root-owned and stays on minimax (fold it there first).
set -eu
root=$(cd "$(dirname "$0")/../../.." && pwd)
art=.local/bbr-loopback-handoff
rsync -a --exclude='*.perf.data' "minimax:agents/bbr-loopback-handoff/$art/observations" "minimax:agents/bbr-loopback-handoff/$art/observations-rerun" \
	"$root/$art/" 2>/dev/null || true
rsync -a --include='*.log' --include='*-summary.json' --exclude='*' "minimax:agents/bbr-loopback-handoff/$art/" "$root/$art/"
