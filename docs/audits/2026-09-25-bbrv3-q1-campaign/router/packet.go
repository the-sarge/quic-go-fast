package main

import "golang.org/x/net/ipv4"

func packetMTU(c config) int {
	if c.Scenario == "gridcast-loss" || c.Scenario == "gridcast-coexistence" {
		return 1500
	}
	return 1460
}

func canonicalPacket(h *ipv4.Header, frameLength, mtu int) bool {
	return h.Len == 20 && h.TotalLen <= mtu && h.TotalLen >= 20 && h.TotalLen+14 <= frameLength && h.FragOff == 0 && h.Flags&ipv4.MoreFragments == 0 && h.TTL >= 2 && (h.Protocol == 1 || h.Protocol == 6 || h.Protocol == 17)
}
