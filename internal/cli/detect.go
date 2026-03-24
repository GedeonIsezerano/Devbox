package cli

import "os"

// marker describes a file whose presence signals a project type.
type marker struct {
	File        string // file to look for in repoRoot
	EnvFile     string // recommended env filename
	ProjectType string // human-readable project type
}

// markers are checked in priority order. Node/TS/framework entries come first
// so they win when multiple marker files are present.
var markers = []marker{
	{"next.config.js", ".env.local", "Next.js"},
	{"next.config.ts", ".env.local", "Next.js"},
	{"next.config.mjs", ".env.local", "Next.js"},
	{"nuxt.config.ts", ".env.local", "Nuxt"},
	{"tsconfig.json", ".env.local", "TypeScript"},
	{"package.json", ".env.local", "Node.js"},
	{"pyproject.toml", ".env", "Python"},
	{"requirements.txt", ".env", "Python"},
	{"go.mod", ".env", "Go"},
	{"Cargo.toml", ".env", "Rust"},
	{"Gemfile", ".env", "Ruby"},
	{"composer.json", ".env", "PHP"},
}

// DetectEnvFile inspects repoRoot for known marker files and returns the
// recommended env filename and the detected project type. If no marker is
// found it returns ".env" with an empty project type.
func DetectEnvFile(repoRoot string) (filename string, projectType string) {
	for _, m := range markers {
		if _, err := os.Stat(repoRoot + "/" + m.File); err == nil {
			return m.EnvFile, m.ProjectType
		}
	}
	return ".env", ""
}
