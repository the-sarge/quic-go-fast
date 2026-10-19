#!/bin/sh
# Build #808's side-stage harness binaries for minimax (linux/amd64).
#
# usage: build.sh [OUTDIR]   (default: <repo>/.local/bbr-808-gso)
#
# Adapted from #824's build.sh (2f550727) with recorded changes: the harness is
# bench808.go.src (rdq824bench plus -cc), and the three trees are
#   send-pre  main at c8dbe35e (post-#742, before #743), unpatched: the K3 baseline sender
#   send      the #808 revision, main at e7eb3f10, unpatched: the sender in every other arm
#   recv      the #808 revision plus #824's recvcount-cand.patch: the receiver in every arm
# Only the receiver uses the dropcount tag. Prints sha256 values and writes
# build.json. GOARCH defaults to amd64 (minimax). A verification aid for #808's
# side stage; not maintained.
set -eu
PRE=c8dbe35eb26fcd69f6f0ebd43b64c15836fcdc80
REV=e7eb3f10d467965d59b06c2a23aba28e6c2788ec
rec=$(cd "$(dirname "$0")" && pwd)
repo=$(git -C "$rec" rev-parse --show-toplevel)
out=${1:-$repo/.local/bbr-808-gso}
[ -e "$out/bin" ] && { echo "$out/bin exists; builds never overwrite" >&2; exit 1; }
mkdir -p "$out/bin"
out=$(cd "$out" && pwd)
for name in send-pre send recv; do
	sha=$REV
	[ "$name" = send-pre ] && sha=$PRE
	rm -rf "$out/trees/$name"
	mkdir -p "$out/trees/$name"
	git -C "$repo" archive "$sha" | tar -x -C "$out/trees/$name"
done
(cd "$out/trees/recv" && git apply "$rec/recvcount-cand.patch")
for name in send-pre send recv; do
	m=$out/mod-$name
	rm -rf "$m"
	mkdir -p "$m"
	cp "$rec/bench808.go.src" "$m/main.go"
	cp "$rec/drops_on.go.src" "$m/drops_on.go"
	cp "$rec/drops_off.go.src" "$m/drops_off.go"
	cp "$out/trees/$name/go.sum" "$m/go.sum"
	cat >"$m/go.mod" <<MOD
module bench808

go 1.26.0

require (
	github.com/quic-go/quic-go v0.0.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
)

require golang.org/x/crypto v0.58.0 // indirect

replace github.com/quic-go/quic-go => $out/trees/$name
MOD
	tags="-tags=dropcount"
	[ "$name" != recv ] && tags=""
	(cd "$m" && GOFLAGS=-mod=mod GOPROXY=off GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
		go build -trimpath -buildvcs=false $tags -o "$out/bin/bench808-$name" .)
done
go version
shasum -a 256 "$out"/bin/bench808-*
python3 - "$out" "$PRE" "$REV" <<'PY'
import hashlib, json, pathlib, subprocess, sys
out, pre, rev = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
bins = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((out / 'bin').glob('bench808-*'))}
go = subprocess.check_output(['go', 'version'], text=True).strip()
(out / 'bin' / 'build.json').write_text(json.dumps(dict(revisions={'send-pre': pre, 'send': rev, 'recv': rev}, sha256=bins, go=go), indent=1) + '\n')
PY
