package ai

import (
	"slices"
	"strings"
	"testing"
)

func TestPermittedCommands(t *testing.T) {
	server := []string{"go test", "dotnet  test"}
	allowed, refused := permittedCommands([]string{"go test ./...", " go   test ", "go vet", "gotest", "dotnet test --no-build", "rm -rf /", ""}, server)
	if !slices.Equal(allowed, []string{"go test ./...", "go   test", "dotnet test --no-build"}) {
		t.Errorf("allowed %q", allowed)
	}
	if !slices.Equal(refused, []string{"go vet", "gotest", "rm -rf /"}) {
		t.Errorf("refused %q", refused)
	}
	if allowed, _ := permittedCommands([]string{"go test"}, nil); len(allowed) != 0 {
		t.Errorf("without a server list nothing may run: %q", allowed)
	}
}

func TestBwrapArgs(t *testing.T) {
	a := agent{bin: "opencode", home: "/data/ai/opencode", config: "/etc/izci/opencode.json"}
	p := sandboxPaths{dir: "/data/ai/work/run1", mirror: "/data/ai/repos/p1.git", state: "/data/ai/sandbox/run1"}
	env := sandboxEnv([]string{"PATH=/usr/bin", "HOME=/home/izci", "ANTHROPIC_API_KEY=k"}, p.state)
	args := bwrapArgs(a, p, "/usr/bin/opencode", []string{"run", "--dir", p.dir}, env)
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{
		" --die-with-parent ", " --unshare-pid ", " --clearenv ", " --tmpfs /tmp ", " --proc /proc ",
		" --ro-bind-try /usr /usr ", " --ro-bind-try /etc/resolv.conf /etc/resolv.conf ",
		" --ro-bind /data/ai/opencode /data/ai/opencode ", " --ro-bind-try /etc/izci/opencode.json /etc/izci/opencode.json ",
		" --bind /data/ai/work/run1 /data/ai/work/run1 ", " --bind /data/ai/repos/p1.git /data/ai/repos/p1.git ",
		" --bind /data/ai/sandbox/run1 /data/ai/sandbox/run1 ", " --chdir /data/ai/work/run1 ",
		" --setenv HOME /data/ai/sandbox/run1/home ", " --setenv XDG_DATA_HOME /data/ai/sandbox/run1/data ",
		" --setenv ANTHROPIC_API_KEY k ", " -- /usr/bin/opencode run --dir /data/ai/work/run1 ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", strings.TrimSpace(want), joined)
		}
	}
	// Neither the whole data directory nor /etc nor the server user's home.
	for _, unwanted := range []string{" /data/ai ", " /etc /etc ", " /home/izci", " --share-net"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("unexpected %q in\n%s", strings.TrimSpace(unwanted), joined)
		}
	}
}
