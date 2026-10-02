package ackhandler

// serviceMechanism names one recovery-service path whose work is bounded
// independently of retained history. Counting is compiled in only with the
// bbrworkcount build tag; ordinary builds inline these hooks away.
type serviceMechanism uint8

const (
	serviceNone serviceMechanism = iota
	serviceAckTransitions
	serviceAckWitness
	servicePTOConfirmation
	servicePersistentSpan
	serviceRetainedDiscovery
	serviceRetainedExpiry
	serviceMechanisms
)
