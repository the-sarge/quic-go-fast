//go:build !windows

package quic

import "testing"

// Only Windows has an optional local-host URO qualification policy.
func requireManagedReceiveCoalescingHost(*testing.T) {}
