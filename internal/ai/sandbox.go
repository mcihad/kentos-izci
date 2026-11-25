package ai

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Sandbox kinds (AI_SANDBOX).
const (
	SandboxNone  = "none"
	SandboxBwrap = "bwrap"
)

// permittedCommands keeps the project's commands that the server allows
// (AI_ALLOWED_COMMANDS): an entry equal to a server command or extending it
// with arguments ("go test ./internal/..." under "go test").
func permittedCommands(project, server []string) (allowed, refused []string) {
	for _, c := range cleanCommands(project) {
		if CommandPermitted(c, server) {
			allowed = append(allowed, c)
		} else {
			refused = append(refused, c)
		}
	}
	return allowed, refused
}

// CommandPermitted reports whether a project command is covered by the
// server's allowlist.
func CommandPermitted(cmd string, server []string) bool {
	cmd = strings.Join(strings.Fields(cmd), " ")
	for _, s := range server {
		s = strings.Join(strings.Fields(s), " ")
		if s != "" && (cmd == s || strings.HasPrefix(cmd, s+" ")) {
			return true
		}
	}
	return false
}

// sandboxEtc are the files of /etc the agent needs: name resolution, TLS
// roots, users, time zone and Debian's alternatives (node, git). The rest
// of /etc (and anything an operator keeps there) stays hidden.
var sandboxEtc = []string{
	"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/host.conf", "/etc/gai.conf", "/etc/passwd", "/etc/group",
	"/etc/localtime", "/etc/ssl", "/etc/ca-certificates", "/etc/pki", "/etc/alternatives", "/etc/ld.so.cache",
	"/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/gitconfig", "/etc/mime.types",
}

// sandboxRoot are system directories shown read-only.
var sandboxRoot = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/opt"}

// sandboxPaths are the writable places of one sandboxed run.
type sandboxPaths struct {
	// dir is the checkout the agent works in.
	dir string
	// mirror is the project's bare repository the checkout belongs to
	// (empty without a repository).
	mirror string
	// state holds HOME and XDG data, cache and state for this run only.
	state string
}

// bwrapArgs wraps an opencode invocation in bubblewrap: new PID, IPC and
// UTS namespaces (izci's own process and environment are not visible),
// system directories read-only, a private /tmp, and only the run's
// checkout, its repository and its own state writable. Other runs,
// other projects' repositories, izci's data and configuration are not
// visible. The network stays shared: the agent talks to its AI provider.
func bwrapArgs(a agent, p sandboxPaths, bin string, args, env []string) []string {
	b := []string{"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try"}
	for _, d := range sandboxRoot {
		b = append(b, "--ro-bind-try", d, d)
	}
	for _, f := range sandboxEtc {
		b = append(b, "--ro-bind-try", f, f)
	}
	b = append(b, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	// opencode installed outside the system directories (its installer
	// uses ~/.opencode).
	if path, err := exec.LookPath(bin); err == nil {
		if real, err := filepath.EvalSymlinks(path); err == nil && !underAny(real, sandboxRoot) {
			b = append(b, "--ro-bind", filepath.Dir(real), filepath.Dir(real))
		}
	}
	b = append(b, "--ro-bind", a.home, a.home)
	if a.config != "" {
		b = append(b, "--ro-bind-try", a.config, a.config)
	}
	b = append(b, "--bind", p.state, p.state)
	// Provider credentials from `opencode auth login`, read-only.
	if auth := opencodeAuthFile(); auth != "" {
		b = append(b, "--ro-bind-try", auth, filepath.Join(p.state, "data", "opencode", "auth.json"))
	}
	if p.mirror != "" {
		b = append(b, "--bind", p.mirror, p.mirror)
	}
	b = append(b, "--bind", p.dir, p.dir, "--chdir", p.dir, "--clearenv")
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			b = append(b, "--setenv", k, v)
		}
	}
	b = append(b, "--", bin)
	return append(b, args...)
}

// sandboxEnv points HOME and the XDG directories (except the read-only
// opencode configuration) at the run's own state directory.
func sandboxEnv(env []string, state string) []string {
	out := make([]string, 0, len(env)+5)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "TMPDIR":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+filepath.Join(state, "home"), "XDG_DATA_HOME="+filepath.Join(state, "data"),
		"XDG_CACHE_HOME="+filepath.Join(state, "cache"), "XDG_STATE_HOME="+filepath.Join(state, "state"), "TMPDIR=/tmp")
}

// prepareSandbox creates the run's state directory.
func prepareSandbox(state string) error {
	for _, d := range []string{"home", "data/opencode", "cache", "state"} {
		if err := os.MkdirAll(filepath.Join(state, d), 0o700); err != nil {
			return err
		}
	}
	return nil
}

// opencodeAuthFile is where `opencode auth login` stores provider
// credentials for the server user, or "" when there is none.
func opencodeAuthFile() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data = filepath.Join(home, ".local", "share")
	}
	f := filepath.Join(data, "opencode", "auth.json")
	if _, err := os.Stat(f); err != nil {
		return ""
	}
	return f
}

func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if path == d || strings.HasPrefix(path, d+"/") {
			return true
		}
	}
	return false
}
