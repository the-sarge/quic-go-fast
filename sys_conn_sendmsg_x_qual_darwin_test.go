//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// resetSendmsgXForTesting clears the process-wide capability state so each
// qualification test starts from an unqualified, unlatched process.
func resetSendmsgXForTesting(t *testing.T) {
	t.Helper()
	reset := func() {
		// Join any in-flight background qualification before replacing the
		// process-wide state.
		sendmsgX.qualifyOnce.Do(func() {})
		sendmsgX.qualifyOnce = sync.Once{}
		sendmsgX.qualifyStarted.Store(false)
		sendmsgX.qualified.Store(false)
		sendmsgX.latched.Store(false)
	}
	reset()
	t.Cleanup(reset)
}

// TestSendmsgXSelfCheckAgainstRunningKernel runs the production-shape
// self-check against the real kernel. On an allowlisted Darwin kernel major
// the self-check must pass (an ABI assumption failure here is the slice's
// operator stop condition); on an unlisted major the check's outcome is
// reported but not asserted, because unlisted majors never activate the
// batch path anyway.
func TestSendmsgXSelfCheckAgainstRunningKernel(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	err = sendmsgXSelfCheck()
	if _, qualified := qualifiedDarwinKernelMajors[major]; !qualified {
		t.Logf("Darwin kernel major %d is not in the qualified set; self-check result: %v", major, err)
		return
	}
	require.NoError(t, err, "self-check must pass on allowlisted Darwin kernel major %d", major)
}

// The kill switch must keep the capability inert without touching the
// syscall.
func TestSendmsgXKillSwitch(t *testing.T) {
	resetSendmsgXForTesting(t)
	t.Setenv(sendmsgXDisableEnv, "true")
	sendmsgXEnsureQualified()
	require.False(t, sendmsgXAvailable())
}

// An unlisted kernel major must fall back, and a listed one must engage
// (given the self-check passes on this host) — the allowlist assertion the
// protocol requires.
func TestSendmsgXKernelMajorAllowlist(t *testing.T) {
	t.Run("unlisted major falls back", func(t *testing.T) {
		resetSendmsgXForTesting(t)
		orig := sendmsgXKernelMajor
		sendmsgXKernelMajor = func() (int, error) { return -1, nil }
		t.Cleanup(func() { sendmsgXKernelMajor = orig })
		sendmsgXEnsureQualified()
		require.False(t, sendmsgXAvailable())
	})
	t.Run("listed major engages after self-check", func(t *testing.T) {
		major, err := getMacOSVersion()
		require.NoError(t, err)
		if _, qualified := qualifiedDarwinKernelMajors[major]; !qualified {
			t.Skipf("running Darwin kernel major %d is not in the qualified set", major)
		}
		resetSendmsgXForTesting(t)
		sendmsgXEnsureQualified()
		require.True(t, sendmsgXAvailable())
	})
}

// ENOSYS latches the capability off for the process.
func TestSendmsgXENOSYSLatch(t *testing.T) {
	resetSendmsgXForTesting(t)
	orig := rawSendmsgX
	rawSendmsgX = func(int, []msghdrX) (int, syscall.Errno) { return 0, syscall.ENOSYS }
	t.Cleanup(func() { rawSendmsgX = orig })

	payloads := [][]byte{[]byte("a"), []byte("b")}
	msgs := make([]msghdrX, 2)
	iovs := make([]syscall.Iovec, 2)
	require.Zero(t, sendmsgXSubmit(-1, payloads, nil, 0, nil, msgs, iovs))
	require.True(t, sendmsgX.latched.Load(), "ENOSYS must latch the capability off")
	require.False(t, sendmsgXAvailable())
}

// A structurally invalid accepted count (out of bounds) latches the
// capability off and reports nothing accepted, so no entry can be skipped.
func TestSendmsgXStructuralLatch(t *testing.T) {
	for name, result := range map[string]struct {
		accepted int
		errno    syscall.Errno
	}{
		"over-acceptance":     {accepted: 3, errno: 0},
		"negative":            {accepted: -1, errno: 0},
		"accepted with errno": {accepted: 1, errno: syscall.EINVAL},
	} {
		t.Run(name, func(t *testing.T) {
			resetSendmsgXForTesting(t)
			orig := rawSendmsgX
			rawSendmsgX = func(int, []msghdrX) (int, syscall.Errno) { return result.accepted, result.errno }
			t.Cleanup(func() { rawSendmsgX = orig })

			payloads := [][]byte{[]byte("a"), []byte("b")}
			msgs := make([]msghdrX, 2)
			iovs := make([]syscall.Iovec, 2)
			require.Zero(t, sendmsgXSubmit(-1, payloads, nil, 0, nil, msgs, iovs))
			require.True(t, sendmsgX.latched.Load())
		})
	}
}

// A transient errno (EAGAIN and friends) reports nothing accepted without
// latching: the worker's per-packet retry owns the entry.
func TestSendmsgXTransientErrnoDoesNotLatch(t *testing.T) {
	resetSendmsgXForTesting(t)
	orig := rawSendmsgX
	rawSendmsgX = func(int, []msghdrX) (int, syscall.Errno) { return 0, syscall.EAGAIN }
	t.Cleanup(func() { rawSendmsgX = orig })

	payloads := [][]byte{[]byte("a"), []byte("b")}
	msgs := make([]msghdrX, 2)
	iovs := make([]syscall.Iovec, 2)
	require.Zero(t, sendmsgXSubmit(-1, payloads, nil, 0, nil, msgs, iovs))
	require.False(t, sendmsgX.latched.Load(), "a transient errno must not latch")
}
