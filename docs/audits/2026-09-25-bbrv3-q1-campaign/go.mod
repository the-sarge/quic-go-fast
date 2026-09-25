// Campaign-only build boundary. Freeze with Q1; archive after Q2.
module q1campaign

go 1.27.0

require (
	github.com/quic-go/quic-go v0.0.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
)

require golang.org/x/crypto v0.57.0 // indirect

replace github.com/quic-go/quic-go => ../../..
