package main

import (
	"encoding/binary"
	"golang.org/x/sys/unix"
)

func ecnBucket(oob []byte) int {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 4
	}
	mark := 4
	for _, msg := range msgs {
		if msg.Header.Level != unix.IPPROTO_IP || msg.Header.Type != unix.IP_TOS {
			continue
		}
		// Linux IP_TOS ancillary payload can be a byte or a native int.
		var value uint32
		switch len(msg.Data) {
		case 1:
			value = uint32(msg.Data[0])
		case 4:
			value = binary.NativeEndian.Uint32(msg.Data)
		default:
			return 4
		}
		if value > 255 || mark != 4 {
			return 4
		}
		mark = int(value & 3)
	}
	return mark
}

func kernelRelease() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return "unknown"
	}
	b := u.Release[:]
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
