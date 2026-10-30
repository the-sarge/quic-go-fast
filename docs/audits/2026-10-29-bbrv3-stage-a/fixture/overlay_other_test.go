//go:build !linux

package main

func markingCases() []markingCase { return []markingCase{{"unknown-native-marking", []byte{1}, 4}} }
