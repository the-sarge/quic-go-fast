#!/bin/sh
# Build the #824 harness binaries for minimax (linux/amd64).
#
# usage: build.sh MAIN_SHA CAND_SHA OUTDIR
#
# GOARCH defaults to amd64 (minimax); set it for a local smoke build.
#
# Exports four source trees with git archive:
#   send  main, unpatched (the sender in every arm)
#   main  main plus recvcount-main.patch (#743's receiver counters)
#   cand  the #824 candidate plus recvcount-cand.patch
#   c128  cand plus depth128.patch (the candidate set back to 128)
# Builds one harness binary per tree; the three receivers use the dropcount
# tag. Prints sha256 values. A verification aid for #824's measurement gate;
# not maintained.
set -eu
main=$1
cand=$2
out=$3
rec=$(cd "$(dirname "$0")" && pwd)
repo=$(git -C "$rec" rev-parse --show-toplevel)
mkdir -p "$out/bin"
out=$(cd "$out" && pwd)
for name in send main cand c128; do
	sha=$main
	case $name in cand | c128) sha=$cand ;; esac
	rm -rf "$out/trees/$name"
	mkdir -p "$out/trees/$name"
	git -C "$repo" archive "$sha" | tar -x -C "$out/trees/$name"
done
(cd "$out/trees/main" && git apply "$rec/recvcount-main.patch")
(cd "$out/trees/cand" && git apply "$rec/recvcount-cand.patch")
(cd "$out/trees/c128" && git apply "$rec/recvcount-cand.patch" && git apply "$rec/depth128.patch")
for name in send main cand c128; do
	m=$out/mod-$name
	rm -rf "$m"
	mkdir -p "$m"
	cp "$rec/rdq824bench.go.src" "$m/main.go"
	cp "$rec/drops_on.go.src" "$m/drops_on.go"
	cp "$rec/drops_off.go.src" "$m/drops_off.go"
	cp "$out/trees/$name/go.sum" "$m/go.sum"
	cat >"$m/go.mod" <<EOF
module rdq824bench

go 1.26.0

require (
	github.com/quic-go/quic-go v0.0.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
)

require golang.org/x/crypto v0.58.0 // indirect

replace github.com/quic-go/quic-go => $out/trees/$name
EOF
	tags="-tags=dropcount"
	[ "$name" = send ] && tags=""
	(cd "$m" && GOFLAGS=-mod=mod GOPROXY=off GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
		go build -trimpath -buildvcs=false $tags -o "$out/bin/rdq824bench-$name" .)
done
go version
shasum -a 256 "$out"/bin/rdq824bench-*
