package main

import (
	"fmt"
	"golang.org/x/sys/windows"
	"net"
)

const socketDropObservation = "unavailable per socket; require before/after host UDP Receive Errors; SocketDrops zero is not evidence"
const ecnObservation = "not requested by Windows rate probe; ECN qualification uses the native QUIC fixture and gateway evidence"

func configureSocket(conn *net.UDPConn, ect bool) (int, error) {
	if ect {
		return 0, fmt.Errorf("Windows rate probe does not qualify ECN; use native QUIC fixture")
	}
	if err := conn.SetReadBuffer(8 << 20); err != nil {
		return 0, err
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var buffer int
	var socketErr error
	err = raw.Control(func(fd uintptr) {
		buffer, socketErr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF)
	})
	if err != nil {
		return 0, err
	}
	return buffer, socketErr
}
func messageTruncated(flags int) bool { return flags&(windows.MSG_TRUNC|windows.MSG_CTRUNC) != 0 }
func readMetadata(oob []byte, r *result) error {
	// This frozen IPv4 rate-probe socket requests no ancillary metadata or URO.
	// Reject unexpected metadata instead of claiming to parse Windows controls.
	if len(oob) > 0 {
		return fmt.Errorf("unexpected control metadata on plain Windows rate probe")
	}
	return nil
}
