//go:build darwin && arm64 && !ios

package quic

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// The libSystem hook raises a locked thread, reads back its base priority and
// restores the thread's class and priority exactly.
func TestSendQoSLibSystemRoundTrip(t *testing.T) {
	type result struct {
		before, during, after int
		prior, restored       sendQoSClass
		errs                  []error
	}
	done := make(chan result)
	go func() {
		var r result
		keep := func(err error) {
			if err != nil {
				r.errs = append(r.errs, err)
			}
		}
		thread := newPlatformSendQoSThread()
		thread.lock()
		defer thread.unlock()
		var err error
		r.before, err = thread.basePriority()
		keep(err)
		r.prior, err = thread.raise()
		keep(err)
		r.during, err = thread.basePriority()
		keep(err)
		keep(thread.restore(r.prior))
		r.after, err = thread.basePriority()
		keep(err)
		r.restored, err = currentSendQoSClass()
		keep(err)
		done <- r
	}()
	r := <-done
	require.Empty(t, r.errs)
	raised := r.during >= sendQoSRaisedPriority
	t.Logf("base priority %d, raised to %d (raised: %t), restored to %d; class %#x", r.before, r.during, raised, r.after, r.prior.class())
	if !raised {
		require.Equal(t, r.before, r.during, "a refused raise leaves the base priority")
	}
	require.Equal(t, r.before, r.after)
	require.Equal(t, r.prior, r.restored)
	switch expect := os.Getenv(sendQoSExpectEnv); expect {
	case "":
	case "raised", "refused":
		require.Equal(t, expect == "raised", raised, "launch context")
	default:
		t.Fatalf("%s=%q: want raised or refused", sendQoSExpectEnv, expect)
	}
}

// A recorded class keeps a negative relative priority.
func TestSendQoSClassEncoding(t *testing.T) {
	c := newSendQoSClass(qosClassUserInitiated, -5)
	require.Equal(t, uint32(qosClassUserInitiated), c.class())
	require.Equal(t, int32(-5), c.relative())
}
