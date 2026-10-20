//go:build !darwin || !arm64 || ios

package quic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendQoSHookIsNoOpOffAppleSilicon(t *testing.T) {
	require.Nil(t, newPlatformSendQoSThread())
}
