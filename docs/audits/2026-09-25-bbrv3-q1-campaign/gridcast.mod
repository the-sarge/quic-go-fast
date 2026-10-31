// Bounded GridCast gate only. Historical go.mod remains unchanged.
module q1campaign

go 1.27.0

require (
	github.com/gopacket/gopacket v1.7.1
	github.com/quic-go/quic-go v0.0.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
)

require golang.org/x/crypto v0.58.0 // indirect

replace github.com/quic-go/quic-go => github.com/the-sarge/quic-go-fast v0.62.1-fast.9
