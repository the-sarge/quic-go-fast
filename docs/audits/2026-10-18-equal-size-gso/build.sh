#!/bin/sh
# Build the #743 harness binaries for minimax (linux/amd64).
#
# usage: build.sh MAIN_SHA PROTO_SHA OUTDIR
#
# GOARCH defaults to amd64 (minimax); set it for a local smoke build.
#
# Exports three source trees with git archive: main (post-#742 main), proto
# (the #743 candidate) and recv (main plus recvcount.patch). Builds one harness
# binary per tree; only recv is built with the dropcount tag. Prints sha256
# values. A verification aid for #743's measurement gate; not maintained.
set -eu
main=$1
proto=$2
out=$3
rec=$(cd "$(dirname "$0")" && pwd)
repo=$(git -C "$rec" rev-parse --show-toplevel)
mkdir -p "$out/bin"
out=$(cd "$out" && pwd)
for name in main proto recv; do
	sha=$main
	[ "$name" = proto ] && sha=$proto
	rm -rf "$out/trees/$name"
	mkdir -p "$out/trees/$name"
	git -C "$repo" archive "$sha" | tar -x -C "$out/trees/$name"
done
(cd "$out/trees/recv" && git apply "$rec/recvcount.patch")
for name in main proto recv; do
	m=$out/mod-$name
	rm -rf "$m"
	mkdir -p "$m"
	cp "$rec/gso743bench.go.txt" "$m/main.go"
	cp "$rec/drops_on.go.txt" "$m/drops_on.go"
	cp "$rec/drops_off.go.txt" "$m/drops_off.go"
	cp "$out/trees/$name/go.sum" "$m/go.sum"
	cat >"$m/go.mod" <<EOF
module gso743bench

go 1.26.0

require (
	github.com/quic-go/quic-go v0.0.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
)

require golang.org/x/crypto v0.58.0 // indirect

replace github.com/quic-go/quic-go => $out/trees/$name
EOF
	tags=""
	[ "$name" = recv ] && tags="-tags=dropcount"
	(cd "$m" && GOFLAGS=-mod=mod GOPROXY=off GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
		go build -trimpath -buildvcs=false $tags -o "$out/bin/gso743bench-$name" .)
done
go version
shasum -a 256 "$out"/bin/gso743bench-*
