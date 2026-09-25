package main

import "golang.org/x/sys/unix"

var socketOptions = [][3]int{{unix.SOL_SOCKET, unix.SO_RCVBUF, 4 << 20}, {unix.IPPROTO_IP, unix.IP_RECVTOS, 1}}

// Darwin returns IP_RECVTOS, not Linux's IP_TOS, as the ancillary type.
// https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man4/ip.4
const receivedTOS = unix.IP_RECVTOS
const receivedOverflow = -1 // Darwin has no Linux SO_RXQ_OVFL ancillary record.
const socketDropObservation = "unavailable per socket; require before/after host-wide UDP full-buffer drop counters; SocketDrops zero is not evidence"
