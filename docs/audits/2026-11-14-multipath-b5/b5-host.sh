#!/usr/bin/env bash
# B5 #1044's counted campaign on the Linux measurement host: one invocation, no reruns
# beyond each driver's contamination rule. It builds the candidate's binaries and peer
# image from an exact source archive, then runs the six BBR interop cases, the twelve
# BBR ordinary-cost observations, the cause-recorded Reno mixed-bulk rerun and the 54
# aggregation instances, charging every attempt against the remaining program ledger.
set -euo pipefail

usage() {
	echo "usage: $0 CANDIDATE_SHA SOURCE_TREE SOURCE_ARCHIVE OUTPUT_DIR REMAINING_INSTANCES REMAINING_HOST_SECONDS" >&2
	exit 2
}
[ $# -eq 6 ] || usage
candidate=$1 tree=$2 archive=$3 out=$4 remaining_instances=$5 remaining_host_seconds=$6
[[ $candidate =~ ^[0-9a-f]{40}$ && $tree =~ ^[0-9a-f]{40}$ ]] || usage
[ ! -e "$out" ] || { echo "output $out exists" >&2; exit 2; }
mkdir -p "$out"
exec > >(tee -a "$out/host.log") 2>&1

reno_cause="B4 #1043 replaced the multipath path views and selection in packet_emission_path.go, and B7 #1357 added per-call ACK carriage to every multipath emission; the opted-in profile's Reno multipath background in this cell runs both."
src="$out/src"
mkdir -p "$src"
tar -xzf "$archive" -C "$src"
sha256sum "$archive" > "$out/source-archive.sha256"
{
	uname -a
	cat /proc/sys/kernel/random/boot_id
	go version
	podman version --format '{{.Server.Version}}'
	python3 --version
	lscpu | grep -E 'Model name|^CPU\(s\)|Thread'
} > "$out/venue.txt"

setup_begun=$(date +%s.%N)
(cd "$src" && CGO_ENABLED=0 GOAMD64=v1 go test -c -tags aggregation -o "$out/aggregation.test" .)
(cd "$src" && CGO_ENABLED=0 GOAMD64=v1 go test -c -tags ordinarycost -o "$out/ordinarycost.test" .)
timeout 1200s podman build --platform linux/amd64 -f "$src/interop/multipath/Dockerfile" \
	--build-arg QGF_SOURCE_COMMIT="$candidate" --build-arg QGF_SOURCE_TREE="$tree" \
	-t qgf-b5 "$src" > "$out/image-build.log" 2>&1
image="sha256:$(podman image inspect qgf-b5 --format '{{.Id}}')"
echo "$image" > "$out/image-id.txt"
setup_seconds=$(echo "$(date +%s.%N) - $setup_begun" | bc)
echo "setup_seconds $setup_seconds"

charged_instances=0
charged_seconds=0
charge() { # instances seconds
	charged_instances=$((charged_instances + $1))
	charged_seconds=$(echo "$charged_seconds + $2" | bc)
	echo "{\"instances\": $charged_instances, \"host_seconds\": $charged_seconds, \"setup_seconds\": $setup_seconds}" > "$out/charged.json"
}
charge 0 "$setup_seconds"
left_instances() { echo $((remaining_instances - charged_instances)); }
left_seconds() { echo "$remaining_host_seconds - $charged_seconds" | bc; }

# Six BBR interop cases, each one charged instance, bounded like the required job.
interop="$out/interop"
mkdir -p "$interop"
for scenario in useful-transfer abandon-reopen-late-feedback same-id-migration-rebinding; do
	for role in client server; do
		[ "$(left_instances)" -ge 1 ] || { echo "insufficient instances" >&2; exit 1; }
		case_dir="$interop/$scenario/$role"
		mkdir -p "$case_dir"
		begun=$(date +%s.%N)
		status=0
		timeout 150s podman run --rm --platform linux/amd64 --network none --userns=keep-id \
			-e QGF_IMAGE_ID="$image" -e PYTHONDONTWRITEBYTECODE=1 \
			-e HOME=/tmp -e GOCACHE=/tmp/go-build -e GOPATH=/tmp/go \
			-v "$interop:/results" qgf-b5 \
			--binary /usr/local/bin/qgf-r17-peer --test-binary /src/peer-matrix.test \
			--output "/results/$scenario/$role/run" --matrix --scenario "$scenario" --role "$role" \
			--controller bbrv3 > "$case_dir/container.log" 2>&1 || status=$?
		elapsed=$(echo "$(date +%s.%N) - $begun" | bc)
		charge 1 "$elapsed"
		echo "{\"scenario\": \"$scenario\", \"role\": \"$role\", \"exit\": $status, \"occupied_s\": $elapsed}" >> "$interop/attempts.jsonl"
		echo "interop $scenario/$role exit $status in $elapsed s"
		if [ -d "$case_dir/run" ]; then
			mv "$case_dir/run/"* "$case_dir/"
			rmdir "$case_dir/run"
		fi
	done
done
python3 -B "$src/interop/multipath/run.py" --validate-matrix --controller bbrv3 --output "$interop" > "$interop/validate.log" 2>&1 \
	&& echo "interop validation pass" || echo "interop validation FAILED"

ledger_charge() { # driver output dir
	python3 - "$1" <<'PY'
import json, sys
ledger = json.load(open(sys.argv[1] + "/ledger.json"))
print(ledger["instances_charged"], ledger["host_seconds_charged"])
PY
}

run_ordinary() { # name, extra driver args...
	local name=$1
	shift
	local status=0
	python3 -B "$src/interop/multipath/ordinary_cost.py" --candidate "$candidate" --binary "$out/ordinarycost.test" \
		--output "$out/$name" --remaining-instances "$(left_instances)" --remaining-host-seconds "$(left_seconds)" \
		--timeout-seconds 135 "$@" || status=$?
	read -r instances seconds < <(ledger_charge "$out/$name")
	charge "$instances" "$seconds"
	echo "$name exit $status, charged $instances instances / $seconds s"
}
run_ordinary ordinary-bbr --controller bbrv3
run_ordinary ordinary-reno-mixed --revalidate-cell mixed-bulk --cause "$reno_cause"

status=0
python3 -B "$src/interop/multipath/aggregation.py" --candidate "$candidate" --binary "$out/aggregation.test" \
	--output "$out/aggregation" --remaining-instances "$(left_instances)" --remaining-host-seconds "$(left_seconds)" \
	--timeout-seconds 120 || status=$?
read -r instances seconds < <(ledger_charge "$out/aggregation")
charge "$instances" "$seconds"
echo "aggregation exit $status, charged $instances instances / $seconds s"
echo "campaign done: $(cat "$out/charged.json")"
