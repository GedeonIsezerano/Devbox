package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectNodeJS(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env.local" {
		t.Errorf("filename = %q, want .env.local", filename)
	}
	if projectType != "Node.js" {
		t.Errorf("projectType = %q, want Node.js", projectType)
	}
}

func TestDetectTypeScript(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env.local" {
		t.Errorf("filename = %q, want .env.local", filename)
	}
	if projectType != "TypeScript" {
		t.Errorf("projectType = %q, want TypeScript", projectType)
	}
}

func TestDetectNextJS(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "next.config.js")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env.local" {
		t.Errorf("filename = %q, want .env.local", filename)
	}
	if projectType != "Next.js" {
		t.Errorf("projectType = %q, want Next.js", projectType)
	}
}

func TestDetectPython(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "pyproject.toml")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "Python" {
		t.Errorf("projectType = %q, want Python", projectType)
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "Go" {
		t.Errorf("projectType = %q, want Go", projectType)
	}
}

func TestDetectRust(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "Cargo.toml")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "Rust" {
		t.Errorf("projectType = %q, want Rust", projectType)
	}
}

func TestDetectRuby(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "Gemfile")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "Ruby" {
		t.Errorf("projectType = %q, want Ruby", projectType)
	}
}

func TestDetectPHP(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "composer.json")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "PHP" {
		t.Errorf("projectType = %q, want PHP", projectType)
	}
}

func TestDetectUnknown(t *testing.T) {
	dir := t.TempDir()

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env" {
		t.Errorf("filename = %q, want .env", filename)
	}
	if projectType != "" {
		t.Errorf("projectType = %q, want empty", projectType)
	}
}

func TestDetectPriority(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json")
	touch(t, dir, "go.mod")

	filename, projectType := DetectEnvFile(dir)
	if filename != ".env.local" {
		t.Errorf("filename = %q, want .env.local (Node wins)", filename)
	}
	if projectType != "Node.js" {
		t.Errorf("projectType = %q, want Node.js (Node wins)", projectType)
	}
}
