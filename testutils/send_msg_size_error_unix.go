//go:build linux || darwin

package testutils

import "golang.org/x/sys/unix"

// SendMsgSizeErr is the native error for an oversized UDP send.
const SendMsgSizeErr = unix.EMSGSIZE
