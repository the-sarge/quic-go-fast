#!/bin/sh
# #740's registered order on minimax, unattended, sequential, under taskset -c 1-3 (adapted from #739's driver).
# Started by a systemd user timer; stdout goes to .local/bbr-r9-memory-attribution/driver.log.
set -eu
cd ~/agents/bbr-r9-memory-attribution/docs/audits/2026-10-08-bbr-r9-memory-attribution
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
for s in smoke perfsmoke ecnsmoke memsmoke; do $T python3 matrix.py $s; done
$T python3 stages.py fold
$T python3 matrix.py hostfacts prereq-end
$T python3 memstages.py preflight
echo "PREFLIGHT-DONE $(date -u +%FT%TZ)"
stage mem-s5
stage mem-s6
stage mem-loopback
stage latcand-stream
$T python3 follow.py reruns
echo "DRIVER-DONE $(date -u +%FT%TZ)"
