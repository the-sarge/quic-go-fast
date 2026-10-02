#!/usr/bin/env bash
# Mutation sweep for the C4 conformance tests. Run from a disposable
# `git archive` extract of the full candidate; it edits files in place and
# restores them after each mutation. Prints caught/SURVIVED per mutation.
set -u
export GOWORK=off
C=internal/congestion
for f in bbr_sender bbr_probe bbr_recovery; do cp "$C/$f.go" "$C/$f.go.orig"; done
restore() { for f in bbr_sender bbr_probe bbr_recovery; do cp "$C/$f.go.orig" "$C/$f.go"; done; }
run() {
  restore
  python3 - "$C/$2" "$3" "$4" <<'EOF' || { echo "$1: NOMATCH"; return; }
import sys
f, a, c = sys.argv[1:4]
s = open(f).read()
if a not in s:
    sys.exit(2)
open(f, "w").write(s.replace(a, c, 1))
EOF
  if go test -count=1 "./$C/" >/dev/null 2>&1; then echo "$1: SURVIVED"; else echo "$1: caught"; fi
}
run "M1 item 2: rejected-path RTT update before the Drain decision" bbr_sender.go $'	if !roundEvidence || !s.Valid || s.Interval <= 0 {\n		b.checkDrainDone(e)' $'	if !roundEvidence || !s.Valid || s.Interval <= 0 {\n		_ = updateRTT()\n		b.checkDrainDone(e)'
run "M2 item 2: early return skips the RTT update" bbr_sender.go $'		updateRTT()\n		return' $'		return'
run "M3 item 2: rejected path drops the expiry result" bbr_sender.go $'		expired := updateRTT()\n		if clockValid {\n			b.checkProbeRTT(e, expired, oldProbeCap)' $'		_ = updateRTT()\n		if clockValid {\n			b.checkProbeRTT(e, false, oldProbeCap)'
run "M4 item 4: drop the latest-volume term" bbr_sender.go 'max(b.bdp(1), bbrBytes(b.latestVolume))' 'b.bdp(1)'
run "M5 item 3: bound growth on a rejected rate" bbr_probe.go $'		if sampleValid {\n			b.inflightLong = max(b.inflightLong, anchor.Delivery.PostInFlight)\n		}' $'		b.inflightLong = max(b.inflightLong, anchor.Delivery.PostInFlight)'
run "M6 item 1: Up undo leaves ProbeRTT" bbr_recovery.go 'b.undo.phase == bbrUp && b.phase != bbrUp && b.phase != bbrProbeRTT' 'b.undo.phase == bbrUp && b.phase != bbrUp'
run "M7 item 2: post-update ProbeRTT cap save" bbr_sender.go $'	expired := updateRTT()\n	if clockValid {\n		b.checkProbeRTT(e, expired, oldProbeCap)' $'	expired := updateRTT()\n	if clockValid {\n		b.checkProbeRTT(e, expired, b.probeRTTTarget())'
run "M8 item 3: no slope raise on a rejected round" bbr_probe.go $'	if sampleValid {\n		b.probeUpAcked' $'	if !sampleValid {\n		return\n	}\n	if sampleValid {\n		b.probeUpAcked'
run "M9 item 1: no CE guard on restoration" bbr_recovery.go 'b.undo.exited && !b.ce.active' 'b.undo.exited'
run "M10 item 1: Startup undo ignores ProbeRTT" bbr_recovery.go $'			case b.undo.phase == bbrStartup && b.phase == bbrProbeRTT:\n				b.probeRTTReturnStartup = true\n' ''
run "M11 item 3: rounds without a valid anchor snapshot" bbr_sender.go 'anchor != nil && anchor.Delivery.Valid &&' 'anchor != nil &&'
run "M12 item 3: rounds on an invalid clock" bbr_sender.go 'roundEvidence := clockValid && anchor' 'roundEvidence := anchor'
run "M13 item 4: quantized learned cap" bbr_sender.go 'max(b.bdp(1), bbrBytes(b.latestVolume))' 'max(b.inflight(1), bbrBytes(b.latestVolume))'
run "M14 item 3: rejected Refill seeds the plateau baseline" bbr_probe.go $'			b.fullBandwidth, b.plateau = 0, 0\n			if sampleValid {\n				b.fullBandwidth = e.Delivery.BytesPerSecond\n			}' $'			b.fullBandwidth, b.plateau = e.Delivery.BytesPerSecond, 0'
restore
for f in bbr_sender bbr_probe bbr_recovery; do rm "$C/$f.go.orig"; done
