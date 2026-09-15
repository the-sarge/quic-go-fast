#!/bin/bash
set -euo pipefail
name=$1
shift
tar -C /source --exclude=.git --exclude=integrationtests --exclude=docs -cf - . | tar -C /workspace -xf -
{
 date -u
 uname -a
 cat /etc/os-release
 id
 go version
 go env GOOS GOARCH GOAMD64 CGO_ENABLED GOTOOLCHAIN
 printenv TIMESCALE_FACTOR
 printf 'GOMAXPROCS=%s\n' "${GOMAXPROCS:-default}"
 printf 'command: timeout --signal=TERM --kill-after=5 595'; printf ' %q' "$@"; printf '\n'
} > "/evidence/$name.environment.log" 2>&1
set +e
timeout --signal=TERM --kill-after=5 595 "$@" > "/evidence/$name.log" 2>&1
result=$?
set -e
printf '%s exit=%s\n' "$name" "$result" > "/evidence/$name.status"
date -u >> "/evidence/$name.status"
cat "/evidence/$name.status"
exit "$result"
