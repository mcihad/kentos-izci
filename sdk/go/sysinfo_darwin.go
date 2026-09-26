//go:build darwin

package izci

import (
	"encoding/binary"
	"strings"
	"syscall"
	"time"
)

func readSystem() hostInfo {
	var h hostInfo
	if v, err := syscall.Sysctl("hw.memsize"); err == nil {
		h.memTotal = sysctlUint64(v)
	}
	if v, err := syscall.Sysctl("kern.osproductversion"); err == nil {
		h.osVersion = strings.TrimSpace(v)
	}
	if v, err := syscall.Sysctl("kern.osrelease"); err == nil {
		h.kernel = strings.TrimSpace(v)
	}
	// vm.loadavg: struct loadavg { fixpt_t ldavg[3]; long fscale; }
	if v, err := syscall.Sysctl("vm.loadavg"); err == nil {
		b := []byte(v)
		if len(b) >= 16 {
			scale := float64(binary.LittleEndian.Uint64(padTo(b[16:], 8)))
			if scale == 0 {
				scale = 2048
			}
			for i := range 3 {
				h.loadAvg = append(h.loadAvg, float64(binary.LittleEndian.Uint32(b[i*4:i*4+4]))/scale)
			}
		}
	}
	return h
}

// sysctlUint64 decodes a numeric sysctl value (syscall.Sysctl returns the
// raw bytes as a string and may drop a trailing zero byte).
func sysctlUint64(v string) uint64 {
	return binary.LittleEndian.Uint64(padTo([]byte(v), 8))
}

func padTo(b []byte, n int) []byte {
	if len(b) >= n {
		return b[:n]
	}
	out := make([]byte, n)
	copy(out, b)
	return out
}

func processUsage() (user, system time.Duration, rss uint64) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		user = time.Duration(ru.Utime.Nano())
		system = time.Duration(ru.Stime.Nano())
		rss = uint64(ru.Maxrss) // bytes on macOS (peak resident set size)
	}
	return user, system, rss
}
