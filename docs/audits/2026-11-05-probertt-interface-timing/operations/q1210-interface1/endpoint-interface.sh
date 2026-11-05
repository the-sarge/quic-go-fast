#!/bin/sh
set -eu
root="$(CDPATH='' cd -- "$(dirname "$0")" && pwd -P)"
export Q1210_KERNEL_TRACE=1
export Q1210_MACH_DENOM=3
export Q1210_MACH_NUMER=125
export Q1210_TRACE=1
export W1460_DIAG=1
export W2200_ARM=interface
export Q1210_OUTPUT_DIR="$root"
exec "$root/wiremux-measure-diagnostic" "$@"
