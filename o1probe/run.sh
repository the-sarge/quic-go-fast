#!/bin/sh
set -eu
exec >"${INFRA_RESULT_PATH}" 2>&1
uname -a
go version
go env GOOS GOARCH
go list -m golang.org/x/sys golang.org/x/net
printf '\nNative IPv4 header receive/send options:\n'
grep -E '^#define[[:space:]]+IP_(RECV|SEND|TOS)' /usr/include/netinet/in.h
printf '\nNative missing-API compilation:\n'
cat >o1probe/api.go <<'EOF'
package o1probe
import "golang.org/x/sys/unix"
var _ = unix.IP_RECVTOS
EOF
if go test ./o1probe -run '^$'; then
  rm o1probe/api.go
  echo 'Unexpected IP_RECVTOS availability; inspect before disposition'
  exit 1
fi
rm o1probe/api.go
printf '\nRED: receive parsing removed (must fail):\n'
if O1_DISABLE_PARSE=1 go test ./o1probe -run '^TestIPv6Receive$' -v -count=1; then exit 1; fi
printf '\nRED: outgoing marking removed (must fail):\n'
if O1_DISABLE_MARK=1 go test ./o1probe -run '^TestIPv6Send$' -v -count=1; then exit 1; fi
printf '\nCharacterization: six classes, normal probe:\n'
go test ./o1probe -v -count=1
printf '\nProbe completed; guest lifecycle cleanup is recorded by infra.\n'
