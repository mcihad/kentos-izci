package ingest

import (
	"strings"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// InAppRules classify frames as application code. Explicit SDK flags win;
// then project rules (include/exclude prefixes of module, filename or
// abs_path); then platform heuristics that recognize runtimes, frameworks and
// dependency directories.
type InAppRules struct {
	Include []string
	Exclude []string
	// Stack holds the project's stack trace rules, whose +app and -app
	// actions override everything else.
	Stack *GroupingRules
}

// libraryModulePrefixes mark modules that are never application code.
var libraryModulePrefixes = map[string][]string{
	"dotnet":     {"System.", "Microsoft.", "Npgsql", "Newtonsoft.", "Dapper", "MediatR", "AutoMapper", "Serilog", "Polly", "Grpc.", "Google.Protobuf", "FluentValidation", "Mapster", "Quartz", "OpenIddict", "Swashbuckle"},
	"java":       {"java.", "javax.", "jdk.", "sun.", "com.sun.", "kotlin.", "kotlinx.", "org.springframework.", "org.apache.", "org.hibernate.", "io.netty.", "com.fasterxml."},
	"kotlin":     {"java.", "javax.", "jdk.", "kotlin.", "kotlinx.", "android.", "androidx."},
	"dart":       {"dart:", "package:flutter/", "package:flutter_", "package:dio/", "package:http/"},
	"python":     {"asyncio", "django.", "flask.", "fastapi.", "starlette.", "sqlalchemy.", "celery."},
	"php":        {"Illuminate\\", "Symfony\\", "Laravel\\", "Doctrine\\", "GuzzleHttp\\"},
	"javascript": {},
	"node":       {},
}

// libraryPathMarkers mark files that are never application code.
var libraryPathMarkers = []string{
	"/node_modules/", "node_modules/", "node:", "internal/process/", "/vendor/", "/go/pkg/mod/",
	"site-packages", "dist-packages", "/lib/python", "webpack/bootstrap", "(webpack)/",
	"/usr/lib/", "/usr/local/lib/", "/.pub-cache/", "/flutter/packages/", "<anonymous>",
	"/rustc/", "/.cargo/registry/", "/Library/Developer/", "/Microsoft.NET/", "/dotnet/shared/",
}

// resolveInApp sets Frame.in_app for frames where the SDK left it unset.
func resolveInApp(frames []*ingestv1.Frame, platform string, rules InAppRules) {
	for _, f := range frames {
		if f.InApp != nil {
			continue
		}
		v := classifyFrame(f, platform, rules)
		f.InApp = &v
	}
	rules.Stack.applyApp(frames)
}

func classifyFrame(f *ingestv1.Frame, platform string, rules InAppRules) bool {
	candidates := []string{f.GetModule(), f.GetFilename(), f.GetAbsPath(), f.GetFunction(), f.GetPackage()}
	if matchesAnyPrefix(candidates, rules.Exclude) {
		return false
	}
	if matchesAnyPrefix(candidates, rules.Include) {
		return true
	}
	fp := f.GetPlatform()
	if fp == "" {
		fp = platform
	}

	path := f.GetAbsPath() + " " + f.GetFilename()
	for _, marker := range libraryPathMarkers {
		if strings.Contains(path, marker) {
			return false
		}
	}

	switch fp {
	case "go":
		return !isGoLibrary(goPackage(f))
	case "native":
		return false
	}
	mod := f.GetModule()
	if mod == "" {
		mod = f.GetFunction()
	}
	for _, prefix := range libraryModulePrefixes[fp] {
		if strings.HasPrefix(mod, prefix) {
			return false
		}
	}
	return true
}

func matchesAnyPrefix(values, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		for _, v := range values {
			if v != "" && strings.HasPrefix(v, p) {
				return true
			}
		}
	}
	return false
}

// goPackage returns the import path of a Go frame: the module field when set,
// otherwise the part of the fully-qualified function name before the last
// "/"-segment's first dot ("github.com/a/b.(*T).M" → "github.com/a/b").
func goPackage(f *ingestv1.Frame) string {
	if m := f.GetModule(); m != "" {
		return m
	}
	fn := f.GetFunction()
	slash := strings.LastIndex(fn, "/")
	if dot := strings.Index(fn[slash+1:], "."); dot >= 0 {
		return fn[:slash+1+dot]
	}
	return fn
}

// isGoLibrary reports whether an import path is library code: the standard
// library (first element without a dot, except "main") or well-known
// infrastructure modules.
func isGoLibrary(pkg string) bool {
	if pkg == "" {
		return false
	}
	first, _, _ := strings.Cut(pkg, "/")
	if strings.Contains(first, ".") {
		// Third-party modules and known framework hosts.
		return strings.HasPrefix(pkg, "golang.org/x/") ||
			strings.HasPrefix(pkg, "google.golang.org/") ||
			strings.HasPrefix(pkg, "connectrpc.com/") ||
			strings.HasPrefix(pkg, "github.com/jackc/pgx")
	}
	return first != "main"
}
