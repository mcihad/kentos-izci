//go:build !linux && !darwin

package izci

import "time"

func readSystem() hostInfo { return hostInfo{} }

func processUsage() (user, system time.Duration, rss uint64) { return 0, 0, 0 }
