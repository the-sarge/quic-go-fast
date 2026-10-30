package main

import "bytes"

var expectedProbePayload = func() [1432 - 16]byte {
	var payload [1432 - 16]byte
	for i := range payload {
		payload[i] = byte(i + 16)
	}
	return payload
}()

func validProbePayload(payload []byte) bool {
	return bytes.Equal(payload, expectedProbePayload[:])
}
