#!/bin/sh
# The port readiness record's registered order (#739's, verbatim apart from paths) on minimax,
# unattended, sequential, under taskset -c 1-3.
# Started by a systemd user timer; stdout goes to .local/bbr-port-readiness/driver.log.
# Registered change from #739 (review finding CLAUDEFABLE-F-001): a failed observation no longer
# stops the order. The stage is completed by resume.py, which keeps the failed attempt, and the
# follow-up steps continue past a failed rerun; follow.py's rerun rule then applies unchanged.
# `resume.py mark` gives an attempt that ended before its receipt a marker receipt, so the rerun
# rule sees it; each follow-up step is retried once after marking. The driver first checks that no
# observation, prerequisite or decision file exists yet.
set -eu
cd ~/agents/bbr-port-readiness/docs/audits/2026-10-16-bbr-port-readiness
T="taskset -c 1-3"
stage() {
	$T python3 matrix.py hostfacts "$1-start"
	echo "STAGE-START $1 $(date -u +%FT%TZ)"
	$T python3 matrix.py "$1" || { echo "STAGE-OBSERVATION-FAILED $1 $(date -u +%FT%TZ)"; $T python3 resume.py "$1"; }
	$T python3 matrix.py hostfacts "$1-end"
	echo "STAGE-END $1 $(date -u +%FT%TZ)"
}
echo "DRIVER-START $(date -u +%FT%TZ)"
A=../../../.local/bbr-port-readiness
for p in "$A"/observations* "$A"/prereq "$A"/hostfacts-* "$A"/follow-decisions.json "$A"/*-summary.json; do
	if [ -e "$p" ]; then echo "PRESTART-NOT-EMPTY $p"; exit 1; fi
done
echo "PRESTART-EMPTY $(date -u +%FT%TZ)"
follow() {
	$T python3 resume.py mark
	$T python3 follow.py "$@" || { echo "FOLLOW-FAILED $* $(date -u +%FT%TZ)"; $T python3 resume.py mark;
		$T python3 follow.py "$@" || echo "FOLLOW-FAILED-AGAIN $* $(date -u +%FT%TZ)"; }
}
$T python3 matrix.py hostfacts prereq-start
$T python3 -c "import art, prereq"
for s in smoke perfsmoke ecnsmoke xsmoke; do $T python3 matrix.py $s; done
$T python3 stages.py fold
$T python3 matrix.py hostfacts prereq-end
echo "PREREQ-DONE $(date -u +%FT%TZ)"
for s in loopback s5 s6; do stage $s; done
follow reruns main
echo "READINESS-DONE $(date -u +%FT%TZ)"
stage s5timeline
follow reruns s5timeline
echo "S5TIMELINE-DONE $(date -u +%FT%TZ)"
follow conditional
echo "DRIVER-DONE $(date -u +%FT%TZ)"
