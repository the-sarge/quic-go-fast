// This consumer is copied into separate upstream and fork main modules.
package main

import (
	"net"
	"os"

	quic "github.com/quic-go/quic-go"
)

type externalPacketIOV1 interface {
	ConfigureExternalPacketIOV1(net.PacketConn, bool, func([][]byte, []byte, *net.UDPAddr) (int, error)) error
	UDPBatchWriterV1(*net.UDPConn) (func([][]byte, []byte, *net.UDPAddr) (int, error), error)
}

func main() {
	_, present := any(&quic.Transport{}).(externalPacketIOV1)
	if len(os.Args) != 2 || present != (os.Args[1] == "fork") {
		panic("unexpected external packet I/O extension availability")
	}
}
