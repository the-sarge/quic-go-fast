//go:build linux || freebsd

package quic

// Read slots above one are a Darwin opt-in (#857). Other batched readers keep
// their platform batch size of pool slots.
func readSlotsFor(OOBCapablePacketConn) (int, bool, error) { return batchSize, false, nil }
