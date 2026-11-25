//go:build linux

package app

import "golang.org/x/sys/unix"

// protectProcess hides izci's memory and environment (/proc/<pid>/environ,
// /proc/<pid>/mem, ptrace) from other processes of the same user, such as
// commands an AI agent runs: the environment holds DATABASE_URL and
// IZCI_SECRET_KEY.
func protectProcess() error { return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }
