#!/bin/sh
# Finite F1 qualification driver for the existing infra native VM runner.
set -eu
uname -a
go version
status=0
if go test -v -count=1 -timeout=5m . -run 'TestManaged|TestOOBReaderAncillaryFailure|TestReadECNFlags|TestSendPacketsWithECNOn'; then
 go test -count=1 -timeout=8m . || status=$?
 if [ "$status" -eq 0 ]; then go vet . || status=$?; fi
 if [ "$status" -eq 0 ]; then CGO_ENABLED=1 go test -race -count=1 -timeout=5m . -run 'TestManaged' || status=$?; fi
else
 status=$?
fi
printf '{"candidate":"%s","test_exit":%s}\n' "$INFRA_CANDIDATE_SHA" "$status" > "$INFRA_RESULT_PATH"
exit "$status"
