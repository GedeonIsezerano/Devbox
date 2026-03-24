package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeRemoteSSH(t *testing.T) {
	got := NormalizeRemoteURL("git@github.com:user/repo.git")
	want := "github.com/user/repo"
	if got != want {
		t.Errorf("NormalizeRemoteURL(SSH) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteHTTPS(t *testing.T) {
	got := NormalizeRemoteURL("https://github.com/user/repo")
	want := "github.com/user/repo"
	if got != want {
		t.Errorf("NormalizeRemoteURL(HTTPS) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteHTTPSWithGit(t *testing.T) {
	got := NormalizeRemoteURL("https://github.com/user/repo.git")
	want := "github.com/user/repo"
	if got != want {
		t.Errorf("NormalizeRemoteURL(HTTPS+.git) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteHTTPWithPort(t *testing.T) {
	got := NormalizeRemoteURL("https://github.com:443/user/repo")
	want := "github.com/user/repo"
	if got != want {
		t.Errorf("NormalizeRemoteURL(HTTPS+port) = %q, want %q", got, want)
	}
}

func TestGetRemoteURL(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo and set an origin remote.
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "remote", "add", "origin", "https://github.com/user/repo.git"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	got, err := GetRemoteURL(dir)
	if err != nil {
		t.Fatalf("GetRemoteURL() error: %v", err)
	}
	want := "https://github.com/user/repo.git"
	if got != want {
		t.Errorf("GetRemoteURL() = %q, want %q", got, want)
	}
}

func TestGetRepoRoot(t *testing.T) {
	dir := t.TempDir()

	// Resolve symlinks so the path matches what git returns.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "init").CombinedOutput()
	if err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Create a subdirectory and ask for the root from there.
	sub := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := GetRepoRoot(sub)
	if err != nil {
		t.Fatalf("GetRepoRoot() error: %v", err)
	}
	if got != dir {
		t.Errorf("GetRepoRoot() = %q, want %q", got, dir)
	}
}
