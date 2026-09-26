package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Check is one item of the setup report.
type Check struct {
	Name   string
	OK     bool
	Detail string
	// Optional checks do not block analyses when they fail.
	Optional bool
}

// Version returns the installed opencode version ("" when opencode cannot
// be run). The result is cached for ten minutes.
func (r *Runner) Version(ctx context.Context) string {
	r.mu.Lock()
	if time.Since(r.version.at) < 10*time.Minute {
		defer r.mu.Unlock()
		return r.version.value
	}
	r.mu.Unlock()
	v, _ := r.command(ctx, 20*time.Second, r.agent.bin, "--version")
	v = strings.TrimSpace(strings.TrimPrefix(firstLine(v), "opencode"))
	r.mu.Lock()
	r.version.value, r.version.at = v, time.Now()
	r.mu.Unlock()
	return v
}

// Doctor checks what AI analyses need, without calling a model.
func (r *Runner) Doctor(ctx context.Context) []Check {
	cfg := r.opts.Config
	var out []Check
	add := func(c Check) { out = append(out, c) }

	if cfg.Enabled {
		add(Check{Name: "AI analysis", OK: true, Detail: "enabled (AI_ENABLED=true)"})
	} else {
		add(Check{Name: "AI analysis", Detail: "disabled; set AI_ENABLED=true"})
	}

	if v, err := r.command(ctx, 10*time.Second, "git", "--version"); err != nil {
		add(Check{Name: "git", Detail: "git was not found in PATH"})
	} else {
		add(Check{Name: "git", OK: true, Detail: strings.TrimSpace(v)})
	}

	version := ""
	if v, err := r.command(ctx, 20*time.Second, r.agent.bin, "--version"); err != nil {
		add(Check{Name: "opencode", Detail: fmt.Sprintf("%q cannot be run (%v); install it or set AI_OPENCODE_BIN", r.agent.bin, err)})
	} else {
		version = strings.TrimSpace(v)
		add(Check{Name: "opencode", OK: true, Detail: "version " + version})
	}

	if version != "" {
		models, err := r.command(ctx, time.Minute, r.agent.bin, "models")
		switch {
		case err != nil:
			add(Check{Name: "model", Detail: "opencode models failed: " + clip(err.Error(), 300)})
		case strings.TrimSpace(models) == "":
			add(Check{Name: "model", Detail: "no model is available; sign in to a provider with: opencode auth login"})
		case cfg.Model == "":
			add(Check{Name: "model", OK: true, Detail: fmt.Sprintf("opencode's default model (%d models available); set AI_OPENCODE_MODEL to pin one",
				len(strings.Fields(models)))})
		case containsLine(models, cfg.Model):
			add(Check{Name: "model", OK: true, Detail: cfg.Model})
		default:
			add(Check{Name: "model", Detail: cfg.Model + " is not available; list models with: opencode models"})
		}
	}

	if cfg.OpencodeConfig != "" {
		if _, err := os.Stat(cfg.OpencodeConfig); err != nil {
			add(Check{Name: "opencode config", Detail: "AI_OPENCODE_CONFIG: " + err.Error()})
		} else {
			add(Check{Name: "opencode config", OK: true, Detail: cfg.OpencodeConfig})
		}
	}

	if err := writable(r.ws.root); err != nil {
		add(Check{Name: "data directory", Detail: err.Error()})
	} else {
		add(Check{Name: "data directory", OK: true, Detail: r.ws.root})
	}

	switch {
	case r.opts.GitHub == nil:
		add(Check{Name: "GitHub", Optional: true, Detail: "not configured; analyses run without repositories"})
	case r.opts.GitHub.App != nil:
		add(Check{Name: "GitHub", OK: true, Optional: true, Detail: "tokens and GitHub App " + r.opts.GitHub.App.Slug})
	default:
		add(Check{Name: "GitHub", OK: true, Optional: true, Detail: "tokens (no GitHub App configured)"})
	}
	return out
}

// command runs a helper program with the agent's environment.
func (r *Runner) command(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(baseEnv(), "XDG_CONFIG_HOME="+r.agent.home, "OPENCODE_DISABLE_AUTOUPDATE=1")
	if r.agent.config != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG="+r.agent.config)
	}
	_ = os.MkdirAll(r.agent.home, 0o750)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, lastLines(msg, 3))
		}
		return "", err
	}
	return stdout.String(), nil
}

func containsLine(s, want string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(filepath.Clean(name))
}
