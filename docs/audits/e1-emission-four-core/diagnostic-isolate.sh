#!/usr/bin/env bash
# Disposable minimax capture launcher. Restores initially unrestricted slices.
# The independent timer also restores them if the SSH session is lost.
set -euo pipefail
[[ $# == 2 ]] || { echo 'usage: isolate.sh OUTPUT smoke|campaign|qlog|bench' >&2; exit 2; }
output=$1
mode=$2
[[ $output == /home/josh/.cache/qgf-e1-four-core/* && ! -e $output ]] || exit 2
for slice in machine.slice system.slice user.slice; do
  [[ -z $(systemctl show "$slice" --property=AllowedCPUs --value) ]] || {
    echo "Unexpected existing CPU restriction on $slice; leaving it intact." >&2
    exit 1
  }
done
[[ ! -e /run/qgf-e1-cpu-reservation && ! -e /run/qgf-e1-four-core-cpu-reservation ]] || { echo 'E1 reservation already exists' >&2; exit 1; }
touch /run/qgf-e1-four-core-cpu-reservation
restore() {
  local failed=0
  for slice in machine.slice system.slice user.slice; do
    systemctl set-property --runtime "$slice" AllowedCPUs= || failed=1
  done
  if (( failed == 0 )); then
    rm /run/qgf-e1-four-core-cpu-reservation
    systemctl stop qgf-e1-four-core-cpu-restore.timer || true
  fi
  return "$failed"
}
trap restore EXIT
trap 'exit 130' INT TERM HUP
systemd-run --unit=qgf-e1-four-core-cpu-restore --on-active=180m --timer-property=AccuracySec=1s \
  /bin/bash -c 'for slice in machine.slice system.slice user.slice; do systemctl set-property --runtime "$slice" AllowedCPUs= || exit 1; done; rm -f /run/qgf-e1-four-core-cpu-reservation'
for slice in machine.slice system.slice user.slice; do
  systemctl set-property --runtime "$slice" AllowedCPUs=0-7,16-23
done
args=()
runner=(/usr/bin/python3 /home/josh/.cache/qgf-e1-four-core/collect.py --output "$output")
case "$mode" in
  smoke) args=(--smoke) ;;
  qlog) args=(--smoke --qlog --cells P3) ;;
  campaign) ;;
  diag) runner=(/usr/bin/python3 /home/josh/.cache/qgf-e1-four-core/diagnose.py "$output") ;;
  profile) runner=(/usr/bin/python3 /home/josh/.cache/qgf-e1-four-core/diagnose.py "$output" --profile) ;;
  bench) runner=(/usr/bin/python3 /home/josh/.cache/qgf-e1-four-core/bench.py "$output") ;;
  *) exit 2 ;;
esac
systemd-run --collect --wait --pipe --unit=qgf-e1-four-core-capture --slice=qgf-e1-four-core.slice \
  --property=User=josh --property=AllowedCPUs=8-15 --property=RuntimeMaxSec=9500 \
  --setenv=PATH=/usr/local/bin:/usr/bin:/bin \
  "${runner[@]}" "${args[@]}"
