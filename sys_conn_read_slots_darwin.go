//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"fmt"
	"os"
	"strconv"
)

// recvmsgXDisableEnv is the read-slot kill switch, mirroring
// QUIC_GO_DISABLE_SENDMSG_X. quic-go never calls recvmsg_x itself; the name
// says what an opted-in adapter does with the slots (#857).
const recvmsgXDisableEnv = "QUIC_GO_DISABLE_RECVMSG_X"

// maxReadSlots bounds a declared slot count. oobConn.readPos is a uint8.
const maxReadSlots = 64

// readSlotsFor returns how many read slots a batch adapter is offered and
// whether they come from the read-slot tier. Only an adapter declaring more
// than one gets them; everything else keeps Darwin's single pool slot.
func readSlotsFor(c OOBCapablePacketConn) (int, bool, error) {
	declared, ok := c.(ReadBatchSlotsV1Conn)
	if !ok {
		return batchSize, false, nil
	}
	if disabled, err := strconv.ParseBool(os.Getenv(recvmsgXDisableEnv)); err == nil && disabled {
		return batchSize, false, nil
	}
	n := declared.ReadBatchSlotsV1()
	if n <= 1 {
		return batchSize, false, nil
	}
	if n > maxReadSlots {
		return 0, false, fmt.Errorf("quic: ReadBatchSlotsV1 returned %d; at most %d read slots are supported", n, maxReadSlots)
	}
	return n, true, nil
}
