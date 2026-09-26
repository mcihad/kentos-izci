package ingest

import (
	"regexp"
	"strings"

	"github.com/medama-io/go-useragent"
)

// UserAgentInfo is a parsed User-Agent header.
type UserAgentInfo struct {
	Browser        string
	BrowserVersion string
	OS             string
	OSVersion      string
	Device         string
	DeviceType     string // desktop, mobile, tablet, tv, bot
	Bot            bool
	Raw            string
}

// BrowserLabel is "Chrome 129" style (major version only).
func (u UserAgentInfo) BrowserLabel() string {
	if u.Browser == "" {
		return ""
	}
	major, _, _ := strings.Cut(u.BrowserVersion, ".")
	return strings.TrimSpace(u.Browser + " " + major)
}

// OSLabel is "Windows 10" / "iOS 17.5" style.
func (u UserAgentInfo) OSLabel() string {
	return strings.TrimSpace(u.OS + " " + u.OSVersion)
}

var uaParser = useragent.NewParser()

var (
	reWindowsNT = regexp.MustCompile(`Windows NT (\d+\.\d+)`)
	reMacOS     = regexp.MustCompile(`Mac OS X (\d+[_.]\d+(?:[_.]\d+)?)`)
	reIOS       = regexp.MustCompile(`(?:iPhone|CPU) OS (\d+(?:_\d+){0,2})`)
	reAndroid   = regexp.MustCompile(`Android (\d+(?:\.\d+){0,2})`)
	reIPhone    = regexp.MustCompile(`\((iPhone|iPad|iPod)`)
	reAndroidDv = regexp.MustCompile(`Android [\d.]+; (?:[a-zA-Z-]+; )?([^;)]+?)(?: Build/|\))`)
)

var windowsVersions = map[string]string{
	"10.0": "10", "6.3": "8.1", "6.2": "8", "6.1": "7", "6.0": "Vista", "5.1": "XP",
}

// ParseUserAgent parses a User-Agent header. It never fails; unknown parts
// stay empty.
func ParseUserAgent(raw string) UserAgentInfo {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return UserAgentInfo{}
	}
	a := uaParser.Parse(raw)
	info := UserAgentInfo{
		Browser:        string(a.Browser()),
		BrowserVersion: a.BrowserVersion(),
		OS:             string(a.OS()),
		Bot:            a.IsBot(),
		Raw:            truncateUTF8(raw, maxHeaderValue),
	}
	if info.OS == "MacOS" {
		info.OS = "macOS"
	}
	switch {
	case a.IsBot():
		info.DeviceType = "bot"
	case a.IsTablet():
		info.DeviceType = "tablet"
	case a.IsMobile():
		info.DeviceType = "mobile"
	case a.IsTV():
		info.DeviceType = "tv"
	case a.IsDesktop():
		info.DeviceType = "desktop"
	}

	switch info.OS {
	case "Windows":
		if m := reWindowsNT.FindStringSubmatch(raw); m != nil {
			if v, ok := windowsVersions[m[1]]; ok {
				info.OSVersion = v
			} else {
				info.OSVersion = m[1]
			}
		}
	case "macOS":
		if m := reMacOS.FindStringSubmatch(raw); m != nil {
			info.OSVersion = strings.ReplaceAll(m[1], "_", ".")
		}
	case "iOS":
		if m := reIOS.FindStringSubmatch(raw); m != nil {
			info.OSVersion = strings.ReplaceAll(m[1], "_", ".")
		}
		if m := reIPhone.FindStringSubmatch(raw); m != nil {
			info.Device = m[1]
		}
	case "Android":
		if m := reAndroid.FindStringSubmatch(raw); m != nil {
			info.OSVersion = m[1]
		}
		if m := reAndroidDv.FindStringSubmatch(raw); m != nil && m[1] != "K" {
			info.Device = strings.TrimSpace(m[1])
		}
	}
	return info
}
