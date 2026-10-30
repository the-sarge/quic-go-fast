//go:build !linux

package main

// Non-Linux builds support local correctness checks only; execute rejects them.
func ecnBucket([]byte) int  { return 4 }
func kernelRelease() string { return "unsupported-for-stage-a" }
