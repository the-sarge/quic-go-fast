package main

import (
	"encoding/binary"
	"golang.org/x/sys/unix"
	"unsafe"
)

func markingCases() []markingCase {
	cases := []markingCase{{"absent", nil, 4}, {"malformed", []byte{1}, 4}}
	for i := 0; i < 4; i++ {
		o := make([]byte, unix.CmsgSpace(4))
		h := (*unix.Cmsghdr)(unsafe.Pointer(&o[0]))
		h.Level = unix.IPPROTO_IP
		h.Type = unix.IP_TOS
		h.SetLen(unix.CmsgLen(4))
		binary.NativeEndian.PutUint32(o[unix.CmsgLen(0):], uint32(i))
		cases = append(cases, markingCase{[]string{"not-ect", "ect1", "ect0", "ce"}[i], o, i})
	}
	return cases
}
