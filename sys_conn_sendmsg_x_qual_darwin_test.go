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

func submitWithFake(t *testing.T, accepted int, errno syscall.Errno) (int, error) {
	t.Helper()
	orig := rawSendmsgX
	rawSendmsgX = func(int, []msghdrX) (int, syscall.Errno) { return accepted, errno }
	t.Cleanup(func() { rawSendmsgX = orig })
	payloads := [][]byte{[]byte("a"), []byte("b")}
	msgs := make([]msghdrX, 2)
	iovs := make([]syscall.Iovec, 2)
	return sendmsgXSubmit(-1, payloads, nil, 0, nil, msgs, iovs)
}

// ENOSYS latches the capability off for the process. The libc error
// convention returns (-1, errno).
func TestSendmsgXENOSYSLatch(t *testing.T) {
	resetSendmsgXForTesting(t)
	accepted, err := submitWithFake(t, -1, syscall.ENOSYS)
	require.Zero(t, accepted)
	require.NoError(t, err, "ENOSYS falls back per-packet; it is not a fatal send error")
	require.True(t, sendmsgX.latched.Load(), "ENOSYS must latch the capability off")
	require.False(t, sendmsgXAvailable())
}

// A structurally invalid accepted count on the success path (ABI drift)
// latches the capability off and fails the send path, because delivery of
// the batch is unknowable.
func TestSendmsgXStructuralLatch(t *testing.T) {
	for name, accepted := range map[string]int{
		"over-acceptance": 3,
		"negative":        -1,
	} {
		t.Run(name, func(t *testing.T) {
			resetSendmsgXForTesting(t)
			got, err := submitWithFake(t, accepted, 0)
			require.Zero(t, got)
			require.Error(t, err, "an unknowable delivery result must fail the send path")
			require.True(t, sendmsgX.latched.Load())
		})
	}
}

// A zero-progress errno — one XNU only reports when no datagram of the
// batch was sent (it suppresses these into a partial count after progress) —
// reports nothing accepted without latching and without a fatal error: the
// worker's per-packet retry owns the first entry with correct attribution.
// The libc convention delivers such errors as (-1, errno).
func TestSendmsgXZeroProgressErrnoDoesNotLatch(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EAGAIN, syscall.EINTR, syscall.ENOBUFS, syscall.EMSGSIZE} {
		t.Run(errno.Error(), func(t *testing.T) {
			resetSendmsgXForTesting(t)
			accepted, err := submitWithFake(t, -1, errno)
			require.Zero(t, accepted)
			require.NoError(t, err)
			require.False(t, sendmsgX.latched.Load(), "a zero-progress errno must not latch")
		})
	}
}

// Any other errno arrives with the accepted count discarded by the libc
// error convention, so kernel progress is unknowable: the submission
// reports a fatal error (the worker must not resend) without latching the
// capability.
func TestSendmsgXUnknownProgressErrnoIsFatal(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EBADF, syscall.ENETDOWN, syscall.EACCES} {
		t.Run(errno.Error(), func(t *testing.T) {
			resetSendmsgXForTesting(t)
			accepted, err := submitWithFake(t, -1, errno)
			require.Zero(t, accepted)
			require.ErrorIs(t, err, errno)
			require.False(t, sendmsgX.latched.Load(), "an ordinary send error must not latch the capability")
		})
	}
}
