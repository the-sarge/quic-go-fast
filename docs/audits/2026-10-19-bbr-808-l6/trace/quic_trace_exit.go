package quic

import "github.com/quic-go/quic-go/internal/congestion"

// Measurement-only #792 overlay: lets the fixture write the BBR trace when it
// exits. Not a maintained hook.
func BBRTraceDumpAtExit() { congestion.TraceDumpAtExit() }
