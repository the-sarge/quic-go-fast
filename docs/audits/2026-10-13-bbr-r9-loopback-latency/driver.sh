#!/bin/sh
# #744's registered order on minimax, unattended, sequential, under taskset -c 1-3 (adapted from #740's driver).
# Started by a systemd user timer; stdout goes to .local/bbr-r9-loopback-latency/driver.log.
set -eu
cd ~/agents/bbr-r9-loopback-latency/docs/audits/2026-10-13-bbr-r9-loopback-latency
T="taskset -c 1-3"
stage() {
	$T python3 matrix.py hostfacts "$1-start"
	echo "STAGE-START $1 $(date -u +%FT%TZ)"
	$T python3 matrix.py "$1"
	$T python3 matrix.py hostfacts "$1-end"
	echo "STAGE-END $1 $(date -u +%FT%TZ)"
}
echo "DRIVER-START $(date -u +%FT%TZ)"
stage syn
$T python3 follow.py reruns syn
stage s1
$T python3 follow.py reruns s1
echo "DRIVER-DONE $(date -u +%FT%TZ)"
