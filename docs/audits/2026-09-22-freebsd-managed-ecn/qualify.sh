#!/bin/sh
# Finite F1 qualification driver for the existing infra native VM runner.
set -eu
uname -a
go version
status=0
go test -v -count=1 -timeout=5m . -run '^TestManagedFreeBSD' || status=$?
printf '{"candidate":"%s","test_exit":%s}\n' "$INFRA_CANDIDATE_SHA" "$status" > "$INFRA_RESULT_PATH"
exit "$status"
