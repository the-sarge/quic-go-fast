#!/bin/sh
# Reproduces the #759 handshake silent-drop policy comparison.
#
# Run from the repository root of a checkout whose product tree equals the
# baseline (only docs/ may differ). The script copies #758's capture and mapping
# fixture, unchanged, and this record's policy file into place, runs them, and
# removes them again. Usage:
#
#   docs/audits/2026-10-17-handshake-silent-drop-policies/reproduce.sh capture OUTPUT_DIR
#   docs/audits/2026-10-17-handshake-silent-drop-policies/reproduce.sh race OUTPUT_DIR
set -eu

BASE=a7b8353ac2e87e716fcfa23119959ff5aeaef648
REC=docs/audits/2026-10-17-handshake-silent-drop-policies
BREC=docs/audits/2026-10-17-handshake-silent-drop-baseline
MODE=${1:?usage: reproduce.sh capture|race OUTPUT_DIR}
OUT=${2:?usage: reproduce.sh capture|race OUTPUT_DIR}

git cat-file -e "$BASE^{commit}"
if ! git diff --quiet "$BASE" -- . ':(exclude)docs'; then
	echo "the product tree differs from $BASE" >&2
	exit 1
fi
# #758's capture fixture, observer and mapping test run unchanged (hashes from its SHA256SUMS).
check() {
	if [ "$(shasum -a 256 "$1" | cut -d' ' -f1)" != "$2" ]; then
		echo "$1 differs from #758's archived fixture" >&2
		exit 1
	fi
}
check "$BREC/fixture/silent_drop_capture_test.go.src" dcfae9da32508f1b203f0f0d3eb77252b4c09759f7d2b5e2750dcff79f72632d
check "$BREC/fixture/silent_drop_mapping_test.go.src" 0a4a53552069553c427c2f8c5dd25ad45831e4152881846c2b4129c3e94555a3
# The mapping test appends to mapping.jsonl, so a reused directory would mix runs.
if [ -e "$OUT" ] && [ -n "$(ls -A "$OUT")" ]; then
	echo "$OUT is not empty; use a new output directory" >&2
	exit 1
fi
# #758's scenario file would duplicate this record's declarations.
for f in silent_drop_capture_test.go silent_drop_baseline_test.go silent_drop_policy_test.go internal/ackhandler/silent_drop_mapping_test.go; do
	if [ -e "$f" ]; then
		echo "$f already exists; refusing to overwrite it" >&2
		exit 1
	fi
done

cp "$BREC/fixture/silent_drop_capture_test.go.src" silent_drop_capture_test.go
cp "$BREC/fixture/silent_drop_mapping_test.go.src" internal/ackhandler/silent_drop_mapping_test.go
cp "$REC/fixture/silent_drop_policy_test.go.src" silent_drop_policy_test.go
trap 'rm -f silent_drop_capture_test.go silent_drop_policy_test.go internal/ackhandler/silent_drop_mapping_test.go' EXIT

mkdir -p "$OUT"
# go test runs each package in its own directory, so the capture path must be absolute.
OUT=$(cd "$OUT" && pwd -P)
export GOTOOLCHAIN=local
{
	echo "mode: $MODE"
	echo "head: $(git rev-parse HEAD)"
	echo "baseline: $BASE"
	go version
	shasum -a 256 silent_drop_capture_test.go silent_drop_policy_test.go internal/ackhandler/silent_drop_mapping_test.go
} >"$OUT/receipt.txt"

TESTS='TestSilentDrop(Policies|PolicyRealUDP|ObserverFidelity|MsgSizeConnFidelity)$'
case "$MODE" in
capture)
	export QUIC_GO_SILENT_DROP_CAPTURE_DIR="$OUT"
	go test -count=1 -v -run 'TestSilentDropTimeoutMapping' ./internal/ackhandler >"$OUT/mapping-test.log" 2>&1
	go test -count=1 -timeout 30m -v -run "$TESTS" . >"$OUT/scenario-test.log" 2>&1
	;;
race)
	go test -race -count=1 -v -run 'TestSilentDropTimeoutMapping' ./internal/ackhandler >"$OUT/mapping-race.log" 2>&1
	go test -race -count=1 -timeout 30m -v -run "$TESTS" . >"$OUT/scenario-race.log" 2>&1
	;;
*)
	echo "unknown mode $MODE" >&2
	exit 1
	;;
esac
echo "ok: $MODE -> $OUT"
