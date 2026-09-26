package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mcihad/kentos-izci/internal/store"
)

// Log entry kinds (izci.api.v1.AiLogKind).
const (
	LogSystem int16 = 1
	LogText   int16 = 2
	LogTool   int16 = 3
	LogStep   int16 = 4
	LogError  int16 = 5
)

// maxEventLine bounds one JSON event of opencode; longer lines (huge tool
// outputs) are skipped.
const maxEventLine = 8 << 20

// agent runs opencode non-interactively.
type agent struct {
	bin   string
	model string
	// home is the isolated configuration directory (XDG_CONFIG_HOME), so
	// that the server user's own opencode setup (MCP servers, plugins,
	// permissions) does not apply to runs.
	home string
	// config is an optional opencode config file of the instance (for
	// example custom providers), set with AI_OPENCODE_CONFIG.
	config string
}

// agentOutcome is what the agent process reported.
type agentOutcome struct {
	usage store.AiUsage
	// errors reported by opencode itself (provider errors, aborted steps).
	errors []string
}

// run starts opencode in dir and streams its events to emit until it
// exits or ctx ends. The process runs in its own process group, which is
// killed as a whole when ctx ends.
func (a agent) run(ctx context.Context, dir, prompt, title string, allowed []string, emit func(kind int16, tool, msg string)) (agentOutcome, error) {
	args := []string{"run", "--format", "json", "--pure", "--auto", "--dir", dir, "--title", title}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	cmd := exec.CommandContext(ctx, a.bin, append(args, prompt)...)
	cmd.Dir = dir
	cmd.Env = append(baseEnv(),
		"XDG_CONFIG_HOME="+a.home,
		"OPENCODE_CONFIG_CONTENT="+opencodeConfig(allowed),
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_SHARE=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		"OPENCODE_DISABLE_CLAUDE_CODE=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	if a.config != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG="+a.config)
	}
	setProcessGroup(cmd)
	cmd.WaitDelay = 15 * time.Second

	var out agentOutcome
	p := &eventParser{dir: dir, emit: emit, outcome: &out}
	stdout := &lineWriter{max: maxEventLine, line: p.handle}
	stderr := &tailBuffer{max: 8 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	killProcessGroup(cmd)
	stdout.flush()
	if ctx.Err() != nil {
		return out, context.Cause(ctx)
	}
	if err != nil {
		msg := lastLines(string(stderr.buf), 5)
		if msg == "" && len(out.errors) > 0 {
			msg = out.errors[len(out.errors)-1]
		}
		return out, fmt.Errorf("opencode exited: %w: %s", err, msg)
	}
	return out, nil
}

// opencodeConfig pins the agent's permissions. Files may be edited inside
// the checkout only; shell commands are denied except the project's
// allowlist; web access, other directories, formatters and language
// servers (which would execute files the agent wrote) are off.
func opencodeConfig(allowed []string) string {
	var bash any = "deny"
	var rules []string
	for _, c := range allowed {
		if c = strings.TrimSpace(c); c != "" {
			rules = append(rules, c)
		}
	}
	if len(rules) > 0 {
		// opencode applies the last matching rule, so the catch-all deny
		// comes first. Rules are written in order, not as a Go map.
		var b bytes.Buffer
		b.WriteString(`{"*":"deny"`)
		for _, c := range rules {
			k, _ := json.Marshal(c + "*")
			b.WriteString("," + string(k) + `:"allow"`)
		}
		b.WriteString("}")
		bash = json.RawMessage(b.Bytes())
	}
	cfg := map[string]any{
		"$schema":    "https://opencode.ai/config.json",
		"share":      "disabled",
		"autoupdate": false,
		"snapshot":   false,
		"formatter":  false,
		"lsp":        false,
		"permission": map[string]any{
			"edit": "allow", "bash": bash, "webfetch": "deny", "websearch": "deny", "codesearch": "deny",
			"question": "deny", "external_directory": "deny", "doom_loop": "deny",
		},
	}
	js, _ := json.Marshal(cfg)
	return string(js)
}

// eventParser turns opencode's JSON events into log entries. The format
// is not a stable API, so unknown events and fields are ignored.
type eventParser struct {
	dir     string
	emit    func(kind int16, tool, msg string)
	outcome *agentOutcome
}

type ocEvent struct {
	Type  string          `json:"type"`
	Part  json.RawMessage `json:"part"`
	Error *struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

type ocToolPart struct {
	Tool  string `json:"tool"`
	State struct {
		Status string         `json:"status"`
		Title  string         `json:"title"`
		Input  map[string]any `json:"input"`
		Error  string         `json:"error"`
	} `json:"state"`
}

type ocStepPart struct {
	Cost   float64 `json:"cost"`
	Tokens struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

func (p *eventParser) handle(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var ev ocEvent
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	switch ev.Type {
	case "text":
		var part struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Part, &part) == nil && strings.TrimSpace(part.Text) != "" {
			p.emit(LogText, "", clip(strings.TrimSpace(part.Text), 8000))
		}
	case "tool_use":
		var part ocToolPart
		if json.Unmarshal(ev.Part, &part) != nil || part.Tool == "" {
			return
		}
		if part.State.Status == "error" {
			p.emit(LogError, part.Tool, clip(p.describe(part)+": "+part.State.Error, 2000))
			return
		}
		p.emit(LogTool, part.Tool, clip(p.describe(part), 2000))
	case "step_finish":
		var part ocStepPart
		if json.Unmarshal(ev.Part, &part) != nil {
			return
		}
		u := &p.outcome.usage
		u.InputTokens += part.Tokens.Input + part.Tokens.Cache.Read + part.Tokens.Cache.Write
		u.OutputTokens += part.Tokens.Output + part.Tokens.Reasoning
		u.CostUSD += part.Cost
		p.emit(LogStep, "", fmt.Sprintf("Adım tamamlandı · %s giriş, %s çıkış token", compact(u.InputTokens), compact(u.OutputTokens)))
	case "error":
		msg := "opencode hatası"
		if ev.Error != nil {
			msg = strings.TrimSpace(ev.Error.Data.Message)
			if msg == "" {
				msg = ev.Error.Name
			}
		}
		p.outcome.errors = append(p.outcome.errors, msg)
		p.emit(LogError, "", clip(msg, 2000))
	}
}

// describe summarizes a tool call, with paths relative to the checkout.
func (p *eventParser) describe(part ocToolPart) string {
	in := part.State.Input
	str := func(k string) string {
		v, _ := in[k].(string)
		return v
	}
	rel := func(path string) string {
		if r, err := filepath.Rel(p.dir, path); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return path
	}
	switch part.Tool {
	case "read", "edit", "write":
		if f := str("filePath"); f != "" {
			return rel(f)
		}
	case "bash":
		if c := str("command"); c != "" {
			return c
		}
	case "grep":
		if s := str("pattern"); s != "" {
			if path := str("path"); path != "" {
				return s + " · " + rel(path)
			}
			return s
		}
	case "glob":
		if s := str("pattern"); s != "" {
			return s
		}
	case "list":
		if s := str("path"); s != "" {
			return rel(s)
		}
		return "."
	case "task":
		if s := str("description"); s != "" {
			return s
		}
	case "todowrite", "todoread":
		return "Yapılacaklar listesi güncellendi"
	}
	if t := strings.TrimSpace(part.State.Title); t != "" {
		return rel(t)
	}
	return part.Tool
}

// lineWriter splits a stream into lines for fn; lines longer than max
// are dropped.
type lineWriter struct {
	max  int
	line func([]byte)
	buf  []byte
	skip bool
}

func (w *lineWriter) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if !w.skip {
				w.buf = append(w.buf, b...)
				if len(w.buf) > w.max {
					w.buf, w.skip = w.buf[:0], true
				}
			}
			break
		}
		if !w.skip && len(w.buf)+i <= w.max {
			w.line(append(w.buf, b[:i]...))
		}
		w.buf, w.skip = w.buf[:0], false
		b = b[i+1:]
	}
	return n, nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 && !w.skip {
		w.line(w.buf)
	}
	w.buf = nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.buf = append(t.buf, b...)
	if len(t.buf) > t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
	}
	return len(b), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return clip(strings.Join(lines, " | "), 1000)
}

// clip shortens s to at most n bytes on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// compact formats token counts: 950, 12,4K, 1,2M.
func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.Replace(strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64), ".", ",", 1) + "M"
	case n >= 1000:
		return strings.Replace(strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64), ".", ",", 1) + "K"
	}
	return strconv.FormatInt(n, 10)
}
