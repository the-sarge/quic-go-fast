//go:build !darwin || !arm64 || ios

package quic

// Only Apple silicon Macs have efficiency cores to keep the send worker off;
// Intel Macs, Rosetta and iOS builds included, every other build has no hook.
func newPlatformSendQoSThread() sendQoSThread { return nil }
