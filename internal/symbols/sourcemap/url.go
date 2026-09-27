package sourcemap

import "strings"

// Resolve joins a reference found in a file (a sourceMappingURL or a source
// map's source) to the URL of that file, like a browser: "../src/a.ts"
// against "https://x.test/assets/app.js" is "https://x.test/src/a.ts". Paths
// never climb above the root. "~/" is a host-less root, as in Sentry artifact
// names. References with a scheme are returned unchanged.
func Resolve(base, ref string) string {
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "~/") {
		return ref
	}
	prefix, path := splitURL(base)
	if strings.HasPrefix(ref, "/") {
		return prefix + cleanPath(ref)
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[:i+1]
	} else {
		path = ""
	}
	if prefix != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	joined := cleanPath(path + ref)
	if prefix == "" && !strings.HasPrefix(path, "/") {
		joined = strings.TrimPrefix(joined, "/")
	}
	return prefix + joined
}

// DisplayPath shortens an original source URL to a host-independent path
// for stack traces: "webpack://app/./src/a.ts", "https://x.test/src/a.ts"
// and "../../src/a.ts" all become "src/a.ts".
func DisplayPath(s string) string {
	if rest, ok := strings.CutPrefix(s, "webpack://"); ok {
		// webpack://<namespace>/<path>
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[i+1:]
		}
		s = rest
	} else if prefix, path := splitURL(s); prefix != "" {
		s = path
	}
	for {
		switch {
		case strings.HasPrefix(s, "/"):
			s = s[1:]
		case strings.HasPrefix(s, "./"):
			s = s[2:]
		case strings.HasPrefix(s, "../"):
			s = s[3:]
		default:
			return s
		}
	}
}

// StripQuery removes a query string and fragment from a URL.
func StripQuery(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		return s[:i]
	}
	return s
}

// HostlessName returns the "~/path" form of a URL, the way Sentry names
// artifacts independently of the host serving them; "" when s has no path.
func HostlessName(s string) string {
	if strings.HasPrefix(s, "~/") {
		return StripQuery(s)
	}
	prefix, path := splitURL(StripQuery(s))
	if prefix == "" || path == "" || path == "/" {
		return ""
	}
	return "~" + path
}

// splitURL splits "scheme://host/path" into "scheme://host" and "/path",
// and "~/path" into "~" and "/path". Other strings have no prefix.
func splitURL(s string) (prefix, path string) {
	if strings.HasPrefix(s, "~/") {
		return "~", StripQuery(s[1:])
	}
	if i := strings.Index(s, "://"); i > 0 {
		rest := s[i+3:]
		j := strings.IndexByte(rest, '/')
		if j < 0 {
			return s, "/"
		}
		return s[:i+3+j], StripQuery(rest[j:])
	}
	return "", StripQuery(s)
}

// cleanPath resolves "." and ".." segments without climbing above the root.
func cleanPath(p string) string {
	abs := strings.HasPrefix(p, "/")
	var out []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	s := strings.Join(out, "/")
	if strings.HasSuffix(p, "/") && s != "" {
		s += "/"
	}
	if abs {
		return "/" + s
	}
	return s
}
