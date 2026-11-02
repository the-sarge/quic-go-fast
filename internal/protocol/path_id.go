package protocol

// PathID identifies a protocol path, independently of its address tuple or generation.
// Draft-ietf-quic-multipath-21 limits it to 32 bits for nonce construction.
type PathID uint32

const MaxPathID PathID = 1<<32 - 1
