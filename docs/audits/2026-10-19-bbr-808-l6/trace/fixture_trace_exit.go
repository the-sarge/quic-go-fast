package main

import quic "github.com/quic-go/quic-go"

// Measurement-only #792 overlay: write the BBR trace after the run's own record
// (so its resource figures do not include the dump). Not a maintained hook.
func bbrTraceDumpAtExit() { quic.BBRTraceDumpAtExit() }
