#!/usr/bin/env bash
# R21 counted native execution on minimax. Usage: r21-host.sh <run-dir>
# The run dir must already hold source.tar (git archive of the exact candidate).
set -euo pipefail
run="$1"
cd "$run"
foreign() { pgrep -af '[R]unner.Worker|[t]ransfer.test|[g]ridcast.test|qemu.*guest=|[n]ativerig' || true; }
{
  echo "== identity"; uname -a; cat /proc/sys/kernel/random/boot_id; id -u
  echo "affinity: $(taskset -pc $$ | sed 's/.*: //')"
  echo "GOMAXPROCS=${GOMAXPROCS-unset} GOGC=${GOGC-unset} GOMEMLIMIT=${GOMEMLIMIT-unset}"
  grep PRETTY_NAME /etc/os-release
  for v in go1.26.11 go1.27.4; do echo "$v: $(GOTOOLCHAIN=$v go env GOVERSION GOROOT | tr '\n' ' ')"; done
  echo "load: $(cat /proc/loadavg)"
  echo "== foreign before"; foreign
} > host-before.log 2>&1
mkdir src && tar -xf source.tar -C src
sha256sum source.tar > source.sha256
cd src
started=$(date +%s.%N)
set +e
python3 -I -B interop/multipath/native_qualification.py --output "$run/out" --candidate "$(cat "$run/candidate")" --profile writer-yield > "$run/driver.stdout" 2> "$run/driver.stderr"
status=$?
set -e
ended=$(date +%s.%N)
cd "$run"
echo "driver_exit $status started $started ended $ended" > driver.status
{
  echo "load: $(cat /proc/loadavg)"
  echo "== foreign after"; foreign
  echo "== leftover test processes"; pgrep -af '[g]o1\.2[67]\.[0-9]*\.test' || echo none
} > host-after.log 2>&1
(cd out && sha256sum go1.26.11.test go1.27.4.test) > binaries.sha256 || true
tar -cf r21-native-results.tar out driver.stdout driver.stderr driver.status host-before.log host-after.log source.sha256 binaries.sha256 candidate
sha256sum r21-native-results.tar > r21-native-results.sha256
echo "done status=$status"
