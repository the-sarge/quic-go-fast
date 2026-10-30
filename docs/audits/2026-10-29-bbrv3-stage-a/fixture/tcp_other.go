//go:build !linux

package main

import "fmt"

func executeTCP(Run, string, string, string, string) error {
	return fmt.Errorf("TCP CUBIC competitor mode is Linux-only")
}
