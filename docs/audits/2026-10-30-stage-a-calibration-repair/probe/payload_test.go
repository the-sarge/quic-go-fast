package main

import "testing"

func TestProbePayloadChecksEveryByte(t *testing.T) {
	payload := make([]byte, 1432-16)
	for i := range payload {
		payload[i] = byte(i + 16)
	}
	if !validProbePayload(payload) {
		t.Fatal("valid wire payload rejected")
	}
	for i := range payload {
		payload[i] ^= 1
		if validProbePayload(payload) {
			t.Fatalf("corruption at wire byte %d accepted", i+16)
		}
		payload[i] ^= 1
	}
	for _, n := range []int{0, 1, len(payload) - 1, len(payload) + 1} {
		wrong := make([]byte, n)
		copy(wrong, payload)
		if validProbePayload(wrong) {
			t.Fatalf("payload length %d accepted", n)
		}
	}
}

func BenchmarkProbePayload(b *testing.B) {
	payload := make([]byte, 1432-16)
	for i := range payload {
		payload[i] = byte(i + 16)
	}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if !validProbePayload(payload) {
			b.Fatal("valid payload rejected")
		}
	}
}
