package ai

import (
	"slices"
	"testing"
)

func TestSensitiveFiles(t *testing.T) {
	sensitive := []string{
		".github/workflows/ci.yml", ".github/dependabot.yml", "services/api/.gitlab-ci.yml", ".gitlab-ci.yml",
		"Jenkinsfile", "Makefile", "build/Dockerfile.prod", "deploy/app.dockerfile", "docker-compose.yml",
		"web/package.json", "web/pnpm-lock.yaml", "go.mod", "go.sum", "src/Kalem.Api/Kalem.Api.csproj",
		"Directory.Build.props", "app/pubspec.yaml", "requirements-dev.txt", "scripts/deploy.sh", "tools\\\\build.ps1",
		".gitmodules", ".vscode/tasks.json", ".devcontainer/devcontainer.json", ".env", ".env.production",
		"docs/CODEOWNERS", "gradle/wrapper/gradle-wrapper.properties", ".husky/pre-commit",
	}
	ordinary := []string{
		"calc.go", "src/Kalem.Api/Controllers/OdemeController.cs", "web/src/routes/index.tsx", "README.md",
		"docs/github.md", "internal/makefile_parser.go", "environment.go", "envelope.json", "requirements.md",
	}
	got := SensitiveFiles(append(append([]string{}, sensitive...), ordinary...))
	for _, p := range sensitive {
		want := p
		if p == "tools\\\\build.ps1" {
			want = "tools/build.ps1"
		}
		if !slices.Contains(got, want) {
			t.Errorf("%s is not reported", p)
		}
	}
	if len(got) != len(sensitive) {
		t.Errorf("reported %d files, want %d: %v", len(got), len(sensitive), got)
	}
}
