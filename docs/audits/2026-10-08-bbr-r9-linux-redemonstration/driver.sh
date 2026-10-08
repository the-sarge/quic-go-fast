#!/bin/sh
# #739's registered order on minimax, unattended, sequential, under taskset -c 1-3.
# Started by a systemd user timer; stdout goes to .local/bbr-r9-linux-redemonstration/driver.log.
set -eu
cd ~/agents/bbr-r9-linux-redemonstration/docs/audits/2026-10-08-bbr-r9-linux-redemonstration
T="taskset -c 1-3"
stage() {
	$T python3 matrix.py hostfacts "$1-start"
	echo "STAGE-START $1 $(date -u +%FT%TZ)"
	$T python3 matrix.py "$1"
	$T python3 matrix.py hostfacts "$1-end"
	echo "STAGE-END $1 $(date -u +%FT%TZ)"
}
echo "DRIVER-START $(date -u +%FT%TZ)"
$T python3 matrix.py hostfacts prereq-start
$T python3 -c "import art, prereq"
for s in smoke perfsmoke ecnsmoke xsmoke; do $T python3 matrix.py $s; done
$T python3 stages.py fold
$T python3 matrix.py hostfacts prereq-end
echo "PREREQ-DONE $(date -u +%FT%TZ)"
for s in loopback s5 s6; do stage $s; done
$T python3 follow.py reruns main
echo "READINESS-DONE $(date -u +%FT%TZ)"
stage s5timeline
$T python3 follow.py reruns s5timeline
echo "S5TIMELINE-DONE $(date -u +%FT%TZ)"
$T python3 follow.py conditional
echo "DRIVER-DONE $(date -u +%FT%TZ)"
