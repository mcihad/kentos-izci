package ai

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mcihad/kentos-izci/internal/store"
)

// sensitiveDirs are directories whose files control what CI, editors and
// git run: a change there executes code with the repository's secrets.
var sensitiveDirs = []string{
	".github/", ".gitlab/", ".circleci/", ".buildkite/", ".azure-pipelines/", ".woodpecker/", ".drone/",
	".husky/", ".githooks/", ".git-hooks/", ".devcontainer/", ".vscode/", ".idea/", ".mvn/", "gradle/wrapper/",
}

// sensitiveNames are files (anywhere in the repository) that define builds,
// dependencies, CI or repository settings.
var sensitiveNames = []string{
	// CI
	".gitlab-ci.yml", "azure-pipelines.yml", "Jenkinsfile", "bitbucket-pipelines.yml", ".drone.yml", ".woodpecker.yml",
	".travis.yml", "appveyor.yml", "cloudbuild.yaml", "cloudbuild.yml", "buildspec.yml",
	// Build and containers
	"Makefile", "GNUmakefile", "makefile", "justfile", "Justfile", "Taskfile.yml", "Taskfile.yaml", "Dockerfile",
	"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml", "Containerfile",
	// Dependencies and package managers
	"package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "yarn.lock",
	".npmrc", ".yarnrc", ".yarnrc.yml", "bun.lock", "bun.lockb", "go.mod", "go.sum", "go.work", "go.work.sum",
	"Directory.Build.props", "Directory.Build.targets", "Directory.Packages.props", "NuGet.Config", "nuget.config",
	"global.json", "pubspec.yaml", "pubspec.lock", "Cargo.toml", "Cargo.lock", "pyproject.toml", "setup.py", "setup.cfg",
	"Pipfile", "Pipfile.lock", "poetry.lock", "uv.lock", "Gemfile", "Gemfile.lock", "composer.json", "composer.lock",
	"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties", "gradlew",
	"gradlew.bat", "pom.xml", "mvnw", "mvnw.cmd",
	// Repository settings and hooks
	".gitmodules", ".gitattributes", "CODEOWNERS", ".pre-commit-config.yaml", "lefthook.yml", ".lefthook.yml",
}

// sensitiveSuffixes match project files and scripts by extension.
var sensitiveSuffixes = []string{
	".csproj", ".fsproj", ".vbproj", ".sln", ".slnx", ".props", ".targets", ".dockerfile", ".mk",
	".sh", ".bash", ".zsh", ".ps1", ".psm1", ".bat", ".cmd",
}

// SensitiveFiles returns the changed files that CI, build tools, package
// managers or git act on (workflows, build scripts, dependency manifests
// and lockfiles, repository settings, environment files). A change there
// runs code with the repository's secrets as soon as CI builds the branch,
// so automatic publishing refuses it and people must confirm it.
func SensitiveFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, `\`, "/")), "/")
		if p == "." || p == "" {
			continue
		}
		base := path.Base(p)
		lower := strings.ToLower(base)
		switch {
		case slices.ContainsFunc(sensitiveDirs, func(d string) bool { return strings.HasPrefix(p+"/", d) || strings.Contains("/"+p, "/"+d) }):
		case slices.Contains(sensitiveNames, base):
		case strings.HasPrefix(lower, "dockerfile"), strings.HasPrefix(lower, "requirements") && strings.HasSuffix(lower, ".txt"):
		case lower == ".env" || strings.HasPrefix(lower, ".env."):
		case slices.ContainsFunc(sensitiveSuffixes, func(s string) bool { return strings.HasSuffix(lower, s) }):
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

// SensitiveChangesError refuses to publish a change to sensitive files
// that nobody confirmed.
type SensitiveChangesError struct{ Files []string }

func (e *SensitiveChangesError) Error() string {
	files := e.Files
	more := ""
	if len(files) > 10 {
		files, more = files[:10], fmt.Sprintf(" and %d more", len(e.Files)-10)
	}
	return fmt.Sprintf("the change touches files that CI, build tools or git run (%s%s); review them and confirm",
		strings.Join(files, ", "), more)
}

func changedPaths(files []store.ChangedFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}
