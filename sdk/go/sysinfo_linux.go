//go:build linux

package izci

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func readSystem() hostInfo {
	var h hostInfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			v *= 1024
			switch fields[0] {
			case "MemTotal:":
				h.memTotal = v
			case "MemFree:":
				h.memFree = v
			case "MemAvailable:":
				h.memAvailable = v
			case "SwapTotal:":
				h.swapTotal = v
			case "SwapFree:":
				h.swapFree = v
			}
		}
		f.Close()
	}
	// Container memory limit (cgroup v2) caps the visible total.
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		if limit, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err == nil && limit > 0 && (h.memTotal == 0 || limit < h.memTotal) {
			if cur, err := os.ReadFile("/sys/fs/cgroup/memory.current"); err == nil {
				if used, err := strconv.ParseUint(strings.TrimSpace(string(cur)), 10, 64); err == nil && used <= limit {
					h.memTotal, h.memAvailable, h.memFree = limit, limit-used, limit-used
				}
			}
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil && len(strings.Fields(string(b))) >= 3 {
		for _, f := range strings.Fields(string(b))[:3] {
			v, _ := strconv.ParseFloat(f, 64)
			h.loadAvg = append(h.loadAvg, v)
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, _ := strings.Cut(line, "=")
			v = strings.Trim(v, `"`)
			switch k {
			case "ID":
				h.distro = v
			case "VERSION_ID":
				h.osVersion = v
			}
		}
	}
	return h
}

func processUsage() (user, system time.Duration, rss uint64) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		user = time.Duration(ru.Utime.Nano())
		system = time.Duration(ru.Stime.Nano())
	}
	if b, err := os.ReadFile("/proc/self/statm"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 1 {
			pages, _ := strconv.ParseUint(f[1], 10, 64)
			rss = pages * uint64(os.Getpagesize())
		}
	}
	return user, system, rss
}
