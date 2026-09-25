package main

import "golang.org/x/sys/unix"

var socketOptions = [][3]int{{unix.SOL_SOCKET, unix.SO_RCVBUF, 4 << 20}, {unix.SOL_SOCKET, unix.SO_RXQ_OVFL, 1}, {unix.IPPROTO_IP, unix.IP_RECVTOS, 1}}

const receivedTOS = unix.IP_TOS
const receivedOverflow = unix.SO_RXQ_OVFL
const socketDropObservation = "per-socket SO_RXQ_OVFL"
