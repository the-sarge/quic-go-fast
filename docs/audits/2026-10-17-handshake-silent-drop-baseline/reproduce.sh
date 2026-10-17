#!/bin/sh
# Reproduces the #758 handshake silent-drop baseline evidence.
#
# Run from the repository root of a checkout whose product tree equals the
# baseline (only docs/ may differ). The script copies the archived fixture into
# place, runs it, and removes it again. Usage:
#
#   docs/audits/2026-10-17-handshake-silent-drop-baseline/reproduce.sh capture OUTPUT_DIR
#   docs/audits/2026-10-17-handshake-silent-drop-baseline/reproduce.sh race OUTPUT_DIR
set -eu

BASE=a7b8353ac2e87e716fcfa23119959ff5aeaef648
REC=docs/audits/2026-10-17-handshake-silent-drop-baseline
MODE=${1:?usage: reproduce.sh capture|race OUTPUT_DIR}
OUT=${2:?usage: reproduce.sh capture|race OUTPUT_DIR}

git cat-file -e "$BASE^{commit}"
if ! git diff --quiet "$BASE" -- . ':(exclude)docs'; then
	echo "the product tree differs from $BASE" >&2
	exit 1
fi
for f in silent_drop_capture_test.go silent_drop_baseline_test.go internal/ackhandler/silent_drop_mapping_test.go; do
	if [ -e "$f" ]; then
		echo "$f already exists; refusing to overwrite it" >&2
		exit 1
	fi
done

cp "$REC/fixture/silent_drop_capture_test.go.src" silent_drop_capture_test.go
cp "$REC/fixture/silent_drop_baseline_test.go.src" silent_drop_baseline_test.go
cp "$REC/fixture/silent_drop_mapping_test.go.src" internal/ackhandler/silent_drop_mapping_test.go
trap 'rm -f silent_drop_capture_test.go silent_drop_baseline_test.go internal/ackhandler/silent_drop_mapping_test.go' EXIT

mkdir -p "$OUT"
export GOTOOLCHAIN=local
{
	echo "mode: $MODE"
	echo "head: $(git rev-parse HEAD)"
	echo "baseline: $BASE"
	go version
	shasum -a 256 silent_drop_capture_test.go silent_drop_baseline_test.go internal/ackhandler/silent_drop_mapping_test.go
} >"$OUT/receipt.txt"

case "$MODE" in
capture)
	export QUIC_GO_SILENT_DROP_CAPTURE_DIR="$OUT"
	go test -count=1 -v -run 'TestSilentDropTimeoutMapping' ./internal/ackhandler >"$OUT/mapping-test.log" 2>&1
	go test -count=1 -v -run 'TestSilentDrop(Baseline|RealUDP|ObserverFidelity)$' . >"$OUT/scenario-test.log" 2>&1
	;;
race)
	go test -race -count=1 -v -run 'TestSilentDropTimeoutMapping' ./internal/ackhandler >"$OUT/mapping-race.log" 2>&1
	go test -race -count=1 -v -run 'TestSilentDrop(Baseline|RealUDP|ObserverFidelity)$' . >"$OUT/scenario-race.log" 2>&1
	;;
*)
	echo "unknown mode $MODE" >&2
	exit 1
	;;
esac
echo "ok: $MODE -> $OUT"
