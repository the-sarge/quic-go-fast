//go:build linux || darwin

package main

import (
	"encoding/binary"
	"golang.org/x/sys/unix"
	"net"
)

const ecnObservation = "native IP traffic-class ancillary data"

func configureSocket(conn *net.UDPConn, ect bool) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var socketErr error
	var buffer int
	err = raw.Control(func(fd uintptr) {
		for _, option := range socketOptions {
			if socketErr = unix.SetsockoptInt(int(fd), option[0], option[1], option[2]); socketErr != nil {
				return
			}
		}
		if ect {
			if socketErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, 2); socketErr != nil {
				return
			}
		}
		buffer, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	})
	if err != nil {
		return 0, err
	}
	return buffer, socketErr
}
func messageTruncated(flags int) bool { return flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 }
func readMetadata(oob []byte, r *result) error {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return err
	}
	for _, m := range messages {
		if m.Header.Level == unix.IPPROTO_IP && m.Header.Type == receivedTOS && len(m.Data) > 0 {
			r.ECN[m.Data[0]&3]++
		}
		if m.Header.Level == unix.SOL_SOCKET && m.Header.Type == receivedOverflow && len(m.Data) >= 4 {
			r.SocketDrops = binary.NativeEndian.Uint32(m.Data[:4])
		}
	}
	return nil
}
