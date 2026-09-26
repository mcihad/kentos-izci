// Package version exposes build metadata injected at link time.
//
// Release builds set the variables with -ldflags, for example:
//
//	go build -ldflags "-X github.com/mcihad/kentos-izci/internal/version.Version=v1.2.3"
//
// Development builds fall back to the VCS information embedded by the Go
// toolchain, so `izci version` is meaningful even without ldflags.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
)

// Values overridden by -ldflags at build time.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	Modified  bool   `json:"modified"`
}

var (
	once sync.Once
	info Info
)

// Get returns the build information, resolving VCS data once.
func Get() Info {
	once.Do(func() {
		info = Info{
			Version:   Version,
			Commit:    Commit,
			Date:      Date,
			GoVersion: runtime.Version(),
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		}
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	})
	return info
}

// ShortCommit returns the first 12 characters of the commit hash.
func (i Info) ShortCommit() string {
	if len(i.Commit) > 12 {
		return i.Commit[:12]
	}
	return i.Commit
}

// String renders a single human-readable line.
func (i Info) String() string {
	commit := i.ShortCommit()
	if commit == "" {
		commit = "unknown"
	}
	if i.Modified {
		commit += "-dirty"
	}
	return fmt.Sprintf("izci %s (commit %s, built %s, %s, %s)", i.Version, commit, orUnknown(i.Date), i.GoVersion, i.Platform)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
