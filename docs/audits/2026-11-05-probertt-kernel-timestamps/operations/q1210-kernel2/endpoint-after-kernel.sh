#!/bin/sh
set -eu
root="$(CDPATH='' cd -- "$(dirname "$0")" && pwd -P)"
export Q1210_TRACE=1
export W1460_DIAG=1
export W2200_ARM=after-kernel
export Q1210_OUTPUT_DIR="$root"
exec "$root/wiremux-measure-diagnostic" "$@"
