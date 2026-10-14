//go:build !bbrworkcount

package ackhandler

func beginServiceWork(serviceMechanism) serviceMechanism { return serviceNone }
func endServiceWork(serviceMechanism)                    {}
func countInspected()                                    {}
func countFailed()                                       {}
func countNodes(int)                                     {}
