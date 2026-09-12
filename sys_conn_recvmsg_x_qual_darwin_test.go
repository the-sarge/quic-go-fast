//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// resetRecvmsgXForTesting clears the process-wide receive capability state so
// each qualification test starts from an unqualified, unlatched process.
func resetRecvmsgXForTesting(t *testing.T) {
	t.Helper()
	reset := func() {
		// Join any in-flight background qualification before replacing the
		// process-wide state.
		recvmsgX.qualifyOnce.Do(func() {})
		recvmsgX.qualifyOnce = sync.Once{}
		recvmsgX.qualifyStarted.Store(false)
		recvmsgX.qualified.Store(false)
		recvmsgX.latched.Store(false)
	}
	reset()
	t.Cleanup(reset)
}

// TestRecvmsgXSelfCheckAgainstRunningKernel runs the production-shape receive
// self-check against the real kernel. On an allowlisted Darwin kernel major
// the self-check must pass (an ABI assumption failure here is the slice's
// operator stop condition); on an unlisted major the check's outcome is
// reported but not asserted, because unlisted majors never activate the
// batch path anyway.
func TestRecvmsgXSelfCheckAgainstRunningKernel(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	err = recvmsgXSelfCheck()
	if _, qualified := qualifiedDarwinKernelMajors[major]; !qualified {
		t.Logf("Darwin kernel major %d is not in the qualified set; self-check result: %v", major, err)
		return
	}
	require.NoError(t, err, "self-check must pass on allowlisted Darwin kernel major %d", major)
}

// The kill switch must keep the receive capability inert without touching
// the syscall.
func TestRecvmsgXKillSwitch(t *testing.T) {
	resetRecvmsgXForTesting(t)
	t.Setenv(recvmsgXDisableEnv, "true")
	recvmsgXEnsureQualified()
	require.False(t, recvmsgXAvailable())
}

// An unlisted kernel major must fall back, and a listed one must engage
// (given the self-check passes on this host) — the allowlist assertion the
// protocol requires. The receive capability shares D1's qualified set.
func TestRecvmsgXKernelMajorAllowlist(t *testing.T) {
	t.Run("unlisted major falls back", func(t *testing.T) {
		resetRecvmsgXForTesting(t)
		orig := recvmsgXKernelMajor
		recvmsgXKernelMajor = func() (int, error) { return -1, nil }
		t.Cleanup(func() { recvmsgXKernelMajor = orig })
		recvmsgXEnsureQualified()
		require.False(t, recvmsgXAvailable())
	})
	t.Run("listed major engages after self-check", func(t *testing.T) {
		major, err := getMacOSVersion()
		require.NoError(t, err)
		if _, qualified := qualifiedDarwinKernelMajors[major]; !qualified {
			t.Skipf("running Darwin kernel major %d is not in the qualified set", major)
		}
		resetRecvmsgXForTesting(t)
		recvmsgXEnsureQualified()
		require.True(t, recvmsgXAvailable())
	})
}
