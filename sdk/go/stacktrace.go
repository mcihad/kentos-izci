package izci

import (
	"bufio"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// sdkPackage is excluded from captured stack traces.
const sdkPackage = "github.com/mcihad/kentos-izci/sdk/go"

// callers captures the current stack, skipping the given number of frames
// above the caller of callers.
func callers(skip int) []uintptr {
	pcs := make([]uintptr, 100)
	n := runtime.Callers(skip+2, pcs)
	return pcs[:n]
}

// stackOf returns the program counters recorded in err by common error
// libraries (github.com/pkg/errors, github.com/go-errors/errors and friends).
func stackOf(err error) []uintptr {
	v := reflect.ValueOf(err)
	// pkg/errors: StackTrace() errors.StackTrace ([]Frame of uintptr).
	if m := v.MethodByName("StackTrace"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 1 {
		out := m.Call(nil)[0]
		if out.Kind() == reflect.Slice {
			pcs := make([]uintptr, 0, out.Len())
			for i := 0; i < out.Len(); i++ {
				e := out.Index(i)
				if e.Kind() == reflect.Uintptr {
					pcs = append(pcs, uintptr(e.Uint()))
				}
			}
			if len(pcs) > 0 {
				return pcs
			}
		}
	}
	// go-errors / others: Callers() []uintptr or StackFrames().
	if c, ok := err.(interface{ Callers() []uintptr }); ok {
		return c.Callers()
	}
	return nil
}

// panicCallers captures the stack inside a deferred recover and cuts it at
// the panic: frames of the recovery machinery (runtime.gopanic and above)
// and the runtime helpers that raised the panic (sigpanic, panicmem,
// mapassign, …) are dropped, so the newest frame is the code that panicked.
func panicCallers() []uintptr {
	pcs := callers(1)
	frames := runtime.CallersFrames(pcs)
	cut := -1
	for i := 0; ; i++ {
		fr, more := frames.Next()
		if fr.Function == "runtime.gopanic" {
			cut = i + 1
			// Skip runtime frames directly below gopanic.
			for more {
				fr, more = frames.Next()
				if !strings.HasPrefix(fr.Function, "runtime.") {
					break
				}
				cut++
			}
			break
		}
		if !more {
			break
		}
	}
	if cut > 0 && cut < len(pcs) {
		return pcs[cut:]
	}
	return pcs
}

// buildStacktrace converts program counters (innermost first, as returned by
// runtime.Callers) into frames ordered oldest first.
func (c *Client) buildStacktrace(pcs []uintptr) *ingestv1.Stacktrace {
	if len(pcs) == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs)
	var out []*ingestv1.Frame
	for {
		fr, more := frames.Next()
		if fr.Function != "" && !strings.HasPrefix(fr.Function, sdkPackage+".") && !strings.HasPrefix(fr.Function, sdkPackage+"/izci") {
			out = append(out, c.frame(fr))
		}
		if !more {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	// runtime order is innermost first; izci expects oldest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return &ingestv1.Stacktrace{Frames: out}
}

func (c *Client) frame(fr runtime.Frame) *ingestv1.Frame {
	pkg, fn := splitFunction(fr.Function)
	f := &ingestv1.Frame{
		Function: fn,
		Module:   pkg,
		AbsPath:  fr.File,
		Filename: relativeFilename(fr.File, pkg),
		Lineno:   uint32(max(fr.Line, 0)),
	}
	inApp := c.inApp(pkg)
	f.InApp = &inApp
	if c.opts.ContextLines && inApp {
		if pre, line, post, ok := sourceContext(fr.File, fr.Line); ok {
			f.PreContext, f.ContextLine, f.PostContext = pre, line, post
		}
	}
	return f
}

// splitFunction splits "github.com/a/b/pkg.(*T).Method" into the import
// path "github.com/a/b/pkg" and "(*T).Method".
func splitFunction(full string) (pkg, fn string) {
	slash := strings.LastIndex(full, "/")
	dot := strings.Index(full[slash+1:], ".")
	if dot < 0 {
		return "", full
	}
	return full[:slash+1+dot], full[slash+2+dot:]
}

// relativeFilename turns an absolute (or -trimpath module-relative) file
// path into a path relative to the module root when the package path is
// visible in it.
func relativeFilename(file, pkg string) string {
	if pkg == "" {
		return file
	}
	// With -trimpath the file already starts with the module path.
	if i := strings.Index(file, pkg+"/"); i >= 0 {
		rel := file[i:]
		// Drop the module path, keep the package-relative directory.
		if mod := modulePrefix(pkg); mod != "" && strings.HasPrefix(rel, mod+"/") {
			return strings.TrimPrefix(rel, mod+"/")
		}
		return rel
	}
	if i := strings.LastIndex(file, "/"); i >= 0 {
		dir := pkg[strings.LastIndex(pkg, "/")+1:]
		return dir + file[i:]
	}
	return file
}

// modulePrefix guesses the module root of a package path using the main
// module path from the build info.
func modulePrefix(pkg string) string {
	if mainModule != "" && strings.HasPrefix(pkg, mainModule) {
		return mainModule
	}
	// Hosted import paths: host/owner/repo.
	parts := strings.SplitN(pkg, "/", 4)
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		return strings.Join(parts[:3], "/")
	}
	return ""
}

func (c *Client) inApp(pkg string) bool {
	if pkg == "" {
		return false
	}
	for _, p := range c.opts.InAppExclude {
		if strings.HasPrefix(pkg, p) {
			return false
		}
	}
	for _, p := range c.opts.InAppInclude {
		if strings.HasPrefix(pkg, p) {
			return true
		}
	}
	first, _, _ := strings.Cut(pkg, "/")
	if !strings.Contains(first, ".") {
		return first == "main" // standard library
	}
	if mainModule != "" {
		return strings.HasPrefix(pkg, mainModule)
	}
	return true
}

var (
	sourceMu    sync.Mutex
	sourceCache = map[string][]string{}
)

// sourceContext reads up to 5 lines around line from file (cached; files
// that do not exist on this machine are skipped quietly).
func sourceContext(file string, line int) (pre []string, cur string, post []string, ok bool) {
	sourceMu.Lock()
	lines, cached := sourceCache[file]
	sourceMu.Unlock()
	if !cached {
		if len(sourceCache) < 500 {
			if f, err := os.Open(file); err == nil {
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					lines = append(lines, sc.Text())
					if len(lines) > 100000 {
						break
					}
				}
				f.Close()
			}
		}
		sourceMu.Lock()
		sourceCache[file] = lines
		sourceMu.Unlock()
	}
	if line < 1 || line > len(lines) {
		return nil, "", nil, false
	}
	i := line - 1
	pre = append([]string(nil), lines[max(0, i-5):i]...)
	post = append([]string(nil), lines[i+1:min(len(lines), i+6)]...)
	return pre, lines[i], post, true
}
