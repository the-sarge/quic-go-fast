//go:build unix && !darwin && !linux && !freebsd

package quic

import "github.com/quic-go/quic-go/internal/handshake"

// These Unix writer builds have no ordinary protection-plan constructor.
// Admission declines an unexpected plan; ordinary construction stays synchronous.
type nativeProtectionPlan struct{}

func (*nativeProtectionPlan) validForMember(endpointMember) bool            { return false }
func (*nativeProtectionPlan) protectAll(*handshake.PacketProtectionScratch) {}
func (*nativeProtectionPlan) release()                                      {}
