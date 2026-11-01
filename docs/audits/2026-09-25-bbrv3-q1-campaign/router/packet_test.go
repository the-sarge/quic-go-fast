package main

import (
	"golang.org/x/net/ipv4"
	"testing"
)

func TestLocalAndHistoricalPacketSizeBoundaries(t *testing.T) {
	for _, scenario := range []string{"gridcast-loss", "gridcast-coexistence", "S6", "L6"} {
		for _, size := range []int{1458, 1460, 1469, 1480, 1500, 1501} {
			h := &ipv4.Header{Version: 4, Len: 20, TotalLen: size, TTL: 64, Protocol: 17}
			want := size <= 1460
			if scenario == "gridcast-loss" || scenario == "gridcast-coexistence" {
				want = size <= 1500
			}
			if got := canonicalPacket(h, 14+size, packetMTU(config{Scenario: scenario})); got != want {
				t.Errorf("%s IP length %d: got %v, want %v", scenario, size, got, want)
			}
		}
	}
}

func TestLocalMTUDoesNotRelaxOtherCanonicalRequirements(t *testing.T) {
	for _, change := range []func(*ipv4.Header){
		func(h *ipv4.Header) { h.Len = 24 },
		func(h *ipv4.Header) { h.TotalLen = 19 },
		func(h *ipv4.Header) { h.FragOff = 1 },
		func(h *ipv4.Header) { h.Flags = ipv4.MoreFragments },
		func(h *ipv4.Header) { h.TTL = 1 },
		func(h *ipv4.Header) { h.Protocol = 2 },
	} {
		h := &ipv4.Header{Version: 4, Len: 20, TotalLen: 1469, TTL: 64, Protocol: 17}
		change(h)
		if canonicalPacket(h, 1514, 1500) {
			t.Fatal("accepted unsupported packet", h)
		}
	}
	h := &ipv4.Header{Version: 4, Len: 20, TotalLen: 1500, TTL: 64, Protocol: 17}
	if canonicalPacket(h, 1513, 1500) {
		t.Fatal("accepted truncated frame")
	}
	for _, protocol := range []int{1, 6, 17} {
		h.Protocol = protocol
		if !canonicalPacket(h, 1514, 1500) {
			t.Fatal("rejected supported protocol", protocol)
		}
	}
}
