//go:build linux

package app

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestProtectProcess(t *testing.T) {
	if err := protectProcess(); err != nil {
		t.Fatal(err)
	}
	if v, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0); err != nil || v != 0 {
		t.Fatalf("dumpable = %d, %v", v, err)
	}
}
