package cli

import (
	"net/url"
	"os/exec"
	"strings"
)

// NormalizeRemoteURL strips protocol, .git suffix, git@ prefix, and port
// numbers from a git remote URL. SSH URLs like git@github.com:user/repo.git
// are converted to github.com/user/repo.
func NormalizeRemoteURL(rawURL string) string {
	// Handle SSH-style: git@host:user/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		rawURL = strings.TrimPrefix(rawURL, "git@")
		// Replace first colon with slash: github.com:user/repo -> github.com/user/repo
		rawURL = strings.Replace(rawURL, ":", "/", 1)
		rawURL = strings.TrimSuffix(rawURL, ".git")
		return rawURL
	}

	// Handle HTTP(S) URLs.
	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback: just strip common prefixes/suffixes.
		rawURL = strings.TrimSuffix(rawURL, ".git")
		return rawURL
	}

	host := u.Hostname() // strips port
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	return host + "/" + path
}

// GetRemoteURL returns the URL of the "origin" remote for the git repo at
// repoPath.
func GetRemoteURL(repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetRepoRoot returns the top-level directory of the git repository
// containing the given path.
func GetRepoRoot(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
