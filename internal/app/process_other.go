//go:build !linux

package app

// protectProcess is a no-op outside Linux.
func protectProcess() error { return nil }
