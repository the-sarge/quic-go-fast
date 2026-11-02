package protocol

// ApplicationKeyReceipt identifies an acknowledged 1-RTT transmission. Its key
// generation is captured at sealing, never inferred from an ACK's carrier or PN.
type ApplicationKeyReceipt struct {
	PathID        PathID
	PacketNumber  PacketNumber
	KeyGeneration KeyPhase
	Valid         bool
}
