//go:build linux

package quic

import (
	"fmt"
	"os"
)

func init() {
	// Dial-owner helper children use stdout as simulated lsof output.
	if os.Getenv("QUIC_GO_DIAL_OWNER_CHILD") != "" {
		return
	}
	major, minor := kernelVersion()
	fmt.Printf("Kernel Version: %d.%d\n\n", major, minor)
}
