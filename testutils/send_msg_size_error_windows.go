package testutils

import "golang.org/x/sys/windows"

// SendMsgSizeErr is the native error for an oversized UDP send.
const SendMsgSizeErr = windows.WSAEMSGSIZE
