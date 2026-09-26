package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// gitAuth is an HTTPS token for git, passed through GIT_CONFIG_* variables
// so that it never appears in command lines or on disk.
type gitAuth struct{ token string }

func (a gitAuth) env() []string {
	if a.token == "" {
		return nil
	}
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+a.token))
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=" + header}
}

// gitCmd describes where and how a git command runs.
//
// Checkouts are edited by the agent, so git never discovers the repository
// from a working directory: gitDir (and workTree) are always explicit, a
// .git file written by the agent is ignored, and global/system
// configuration, hooks, fsmonitor and external diff drivers are disabled.
// Only the mirror's own configuration, written by izci, applies.
type gitCmd struct {
	gitDir   string
	workTree string
	index    string
	auth     gitAuth
	env      []string
	stdin    io.Reader
	// limit caps the captured standard output (default 1 MiB).
	limit int
}

var errOutputTooLarge = errors.New("output too large")

// run runs git and returns its standard output.
func (g gitCmd) run(ctx context.Context, args ...string) ([]byte, error) {
	base := []string{
		"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
		"-c", "protocol.ext.allow=never", "-c", "diff.external=", "-c", "core.pager=cat", "-c", "gc.auto=0",
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = g.gitDir
	if g.workTree != "" {
		cmd.Dir = g.workTree
	}
	cmd.Env = append(baseEnv(), "GIT_DIR="+g.gitDir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "SSH_ASKPASS=true", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if g.workTree != "" {
		cmd.Env = append(cmd.Env, "GIT_WORK_TREE="+g.workTree)
	}
	if g.index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+g.index)
	}
	cmd.Env = append(append(cmd.Env, g.env...), g.auth.env()...)
	limit := g.limit
	if limit == 0 {
		limit = 1 << 20
	}
	stdout := &capWriter{max: limit}
	var stderr capWriter
	stderr.max = 4096
	cmd.Stdout, cmd.Stderr, cmd.Stdin = stdout, &stderr, g.stdin
	err := cmd.Run()
	if stdout.over {
		return nil, fmt.Errorf("git %s: %w", args[0], errOutputTooLarge)
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.buf.String()))
	}
	return stdout.buf.Bytes(), nil
}

// text runs git and returns its trimmed standard output.
func (g gitCmd) text(ctx context.Context, args ...string) (string, error) {
	out, err := g.run(ctx, args...)
	return strings.TrimSpace(string(out)), err
}

// bare returns a command for a bare repository (the mirror).
func bare(dir string, auth gitAuth) gitCmd { return gitCmd{gitDir: dir, auth: auth} }

// capWriter keeps at most max bytes and fails writes beyond that, which
// stops the producing process.
type capWriter struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		w.over = true
		return 0, errOutputTooLarge
	}
	return w.buf.Write(p)
}

// baseEnv is the environment handed to child processes: enough to find
// binaries, credentials of the AI provider and a temp dir, but none of
// izci's own secrets (database, secret key, bot tokens).
func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "PATH" || k == "HOME" || k == "USER" || k == "LOGNAME" || k == "SHELL" || k == "TMPDIR" || k == "TERM" ||
			k == "LANG" || k == "LC_ALL" || k == "TZ" || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "OPENCODE_") ||
			strings.HasSuffix(k, "_API_KEY") || strings.HasPrefix(k, "AWS_") || strings.HasPrefix(k, "GOOGLE_") ||
			strings.HasPrefix(k, "AZURE_") || strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "OPENAI_") ||
			k == "SSL_CERT_FILE" || k == "SSL_CERT_DIR" || k == "HTTPS_PROXY" || k == "HTTP_PROXY" || k == "NO_PROXY":
			out = append(out, kv)
		}
	}
	return out
}

// numstat parses `git diff --numstat -z --no-renames` output. Binary
// files report no line counts.
func numstat(out []byte) map[string][2]int {
	m := map[string][2]int{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		f := strings.SplitN(string(rec), "\t", 3)
		if len(f) != 3 {
			continue
		}
		add, _ := strconv.Atoi(f[0])
		del, _ := strconv.Atoi(f[1])
		m[f[2]] = [2]int{add, del}
	}
	return m
}

// nameStatus parses `git diff --name-status -z --no-renames` output into
// path → status ("added", "modified", "deleted").
func nameStatus(out []byte) (paths []string, status map[string]string) {
	status = map[string]string{}
	f := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	for i := 0; i+1 < len(f); i += 2 {
		p := string(f[i+1])
		switch string(f[i]) {
		case "A":
			status[p] = "added"
		case "D":
			status[p] = "deleted"
		default:
			status[p] = "modified"
		}
		paths = append(paths, p)
	}
	return paths, status
}
