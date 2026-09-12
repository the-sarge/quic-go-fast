#!/bin/bash

set -e

dist="$1"
goos=$(echo "$dist" | cut -d "/" -f1)
goarch=$(echo "$dist" | cut -d "/" -f2)

# cross-compiling for android is a pain...
if [[ "$goos" == "android" ]]; then exit; fi
# iOS binaries require Cgo linking (https://github.com/golang/go/issues/43343),
# which would need a C cross compilation setup. Compiling the library packages
# without Cgo still verifies the build constraints — in particular that the
# sendmsg_x active/stub pair leaves iOS builds without the private-syscall
# path (docs/adr/2026-09-11-darwin-batch-plan.md, slice D1).
if [[ "$goos" == "ios" ]]; then
  log_file=$(mktemp)
  trap 'cat "$log_file" >&2; rm "$log_file"; exit 1' ERR
  echo "$dist (library packages only)" >> "$log_file"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build . ./http3 >> "$log_file" 2>&1
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -tags quic_go_no_private_syscalls . ./http3 >> "$log_file" 2>&1
  cat "$log_file"
  rm "$log_file"
  exit
fi

# Write all log output to a temporary file instead of to stdout.
# That allows running this script in parallel, while preserving the correct order of the output.
log_file=$(mktemp)

error_handler() {
  cat "$log_file" >&2
  rm "$log_file"
  exit 1
}

trap 'error_handler' ERR

echo "$dist" >> "$log_file"
out="main-$goos-$goarch"
GOOS=$goos GOARCH=$goarch go build -o $out example/main.go >> "$log_file" 2>&1
rm $out

cat "$log_file"
rm "$log_file"
