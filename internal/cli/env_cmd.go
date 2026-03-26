package cli

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// PullOptions configures the pull command.
type PullOptions struct {
	Project string
	EnvFile string
	Force   bool
	Diff    bool
	Backup  bool
	Cached  bool
}

// PushOptions configures the push command.
type PushOptions struct {
	Project string
	EnvFile string
	Force   bool
}

// cacheState is persisted in .dbx/cache/state.toml.
type cacheState struct {
	LastPulledVersion int    `toml:"last_pulled_version"`
	EnvFile           string `toml:"env_file"`
}

const maxEnvFileSize = 64 * 1024 // 64KB

// resolveProject finds the project ID either from the explicit --project flag
// or by matching the current git remote URL against known projects.
func resolveProject(projectName string, client *Client, printer *Printer) (*ProjectResponse, error) {
	projects, err := client.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}

	// If explicit project name given, find by name.
	if projectName != "" {
		for i := range projects {
			if projects[i].Name == projectName {
				return &projects[i], nil
			}
		}
		return nil, fmt.Errorf("project %q not found", projectName)
	}

	// Try to match by git remote URL.
	repoRoot, err := GetRepoRoot(".")
	if err != nil {
		return nil, fmt.Errorf("not in a git repository and no --project specified")
	}

	remoteURL, err := GetRemoteURL(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("no git remote 'origin' found and no --project specified")
	}

	normalized := NormalizeRemoteURL(remoteURL)

	for i := range projects {
		if projects[i].RemoteURL == normalized {
			return &projects[i], nil
		}
	}

	return nil, fmt.Errorf("no project found matching remote %q — use --project or run `dbx init`", normalized)
}

// resolveEnvFile determines the target env file path.
func resolveEnvFile(explicit string, project *ProjectResponse, repoRoot string) string {
	if explicit != "" {
		return explicit
	}

	// Try detection.
	detected, _ := DetectEnvFile(repoRoot)
	if detected != ".env" {
		return detected
	}

	// If server has an env_file value, prefer it.
	if project != nil && project.EnvFile != "" {
		return project.EnvFile
	}

	return detected
}

// readCacheState reads .dbx/cache/state.toml from the repo root.
func readCacheState(repoRoot string) (cacheState, error) {
	var state cacheState
	path := filepath.Join(repoRoot, ".dbx", "cache", "state.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := toml.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

// writeCacheState writes .dbx/cache/state.toml.
func writeCacheState(repoRoot string, state cacheState) error {
	dir := filepath.Join(repoRoot, ".dbx", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "state.toml")

	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(state); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// writeCacheBlob writes the env data to .dbx/cache/env.dat.
func writeCacheBlob(repoRoot string, data []byte) error {
	dir := filepath.Join(repoRoot, ".dbx", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "env.dat"), data, 0o600)
}

// readCacheBlob reads .dbx/cache/env.dat.
func readCacheBlob(repoRoot string) ([]byte, error) {
	return os.ReadFile(filepath.Join(repoRoot, ".dbx", "cache", "env.dat"))
}

// diffLines computes a simple line-by-line diff between two strings.
// Returns lines prefixed with +/- and a summary of counts.
func diffLines(old, new string) (lines []string, added, removed, changed int) {
	oldMap := parseEnvLines(old)
	newMap := parseEnvLines(new)

	// Find removed and changed keys.
	for key, oldVal := range oldMap {
		if newVal, ok := newMap[key]; ok {
			if oldVal != newVal {
				changed++
				lines = append(lines, fmt.Sprintf("~ %s", key))
			}
		} else {
			removed++
			lines = append(lines, fmt.Sprintf("- %s", key))
		}
	}

	// Find added keys.
	for key := range newMap {
		if _, ok := oldMap[key]; !ok {
			added++
			lines = append(lines, fmt.Sprintf("+ %s", key))
		}
	}

	return lines, added, removed, changed
}

// parseEnvLines parses KEY=VALUE lines from env file content.
// Ignores comments and blank lines.
func parseEnvLines(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// maskValue masks an env var value, showing only the first and last 2 chars
// if it's long enough, otherwise fully masked.
func maskValue(val string) string {
	if len(val) <= 6 {
		return "****"
	}
	return val[:2] + "****" + val[len(val)-2:]
}

// RunPull performs `dbx pull`.
func RunPull(opts PullOptions, printer *Printer) error {
	repoRoot, err := GetRepoRoot(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Authenticate.
	client, err := ResolveAuth(printer)
	if err != nil {
		// If server unreachable and --cached, try cache.
		if opts.Cached {
			return pullFromCache(repoRoot, opts, printer)
		}
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Resolve project.
	project, err := resolveProject(opts.Project, client, printer)
	if err != nil {
		if opts.Cached {
			return pullFromCache(repoRoot, opts, printer)
		}
		return err
	}

	// Pull env from server.
	envResp, err := client.PullEnv(project.ID)
	if err != nil {
		if opts.Cached {
			return pullFromCache(repoRoot, opts, printer)
		}
		return fmt.Errorf("pulling env vars: %w", err)
	}

	// Decode base64 data.
	plaintext, err := base64.StdEncoding.DecodeString(envResp.Data)
	if err != nil {
		return fmt.Errorf("decoding env data: %w", err)
	}

	// Determine env file.
	envFile := resolveEnvFile(opts.EnvFile, project, repoRoot)
	targetPath := filepath.Join(repoRoot, envFile)

	// Check if target is a symlink.
	if info, lErr := os.Lstat(targetPath); lErr == nil && info.Mode()&os.ModeSymlink != 0 {
		printer.Info("Warning: %s is a symlink", envFile)
	}

	// Check if target exists and differs.
	existing, readErr := os.ReadFile(targetPath)
	if readErr == nil && string(existing) == string(plaintext) {
		printer.Success("Already up to date (%s, version %d)", envFile, envResp.Version)
		// Still update cache.
		if err := writeCacheBlob(repoRoot, plaintext); err != nil {
			log.Printf("warning: failed to write cache blob: %v", err)
		}
		if err := writeCacheState(repoRoot, cacheState{
			LastPulledVersion: envResp.Version,
			EnvFile:           envFile,
		}); err != nil {
			log.Printf("warning: failed to write cache state: %v", err)
		}
		return nil
	}

	if readErr == nil {
		// File exists and differs.
		diffOutput, added, removed, changed := diffLines(string(existing), string(plaintext))

		if opts.Diff {
			// Just show the diff, don't write.
			printer.Info("Diff for %s (server version %d):", envFile, envResp.Version)
			for _, line := range diffOutput {
				fmt.Fprintln(printer.Stdout, line)
			}
			printer.Info("%d added, %d removed, %d changed", added, removed, changed)
			return nil
		}

		if !opts.Force && !printer.IsCI {
			printer.Info("Changes in %s (version %d):", envFile, envResp.Version)
			for _, line := range diffOutput {
				printer.Info("  %s", line)
			}
			printer.Info("%d added, %d removed, %d changed", added, removed, changed)

			fmt.Fprintf(printer.Stderr, "Overwrite %s? [y/N] ", envFile)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				printer.Info("Cancelled")
				return nil
			}
		}

		if opts.Backup {
			backupPath := targetPath + ".backup"
			if bErr := os.WriteFile(backupPath, existing, 0o600); bErr != nil {
				return fmt.Errorf("creating backup: %w", bErr)
			}
			printer.Info("Backup saved to %s.backup", envFile)
		}
	}

	// Write the file.
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(targetPath, plaintext, 0o600); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}

	// Cache the blob and version.
	if err := writeCacheBlob(repoRoot, plaintext); err != nil {
		log.Printf("warning: failed to write cache blob: %v", err)
	}
	if err := writeCacheState(repoRoot, cacheState{
		LastPulledVersion: envResp.Version,
		EnvFile:           envFile,
	}); err != nil {
		log.Printf("warning: failed to write cache state: %v", err)
	}

	// Check .gitignore.
	checkGitignore(repoRoot, envFile, printer)

	printer.Success("Pulled %s (version %d)", envFile, envResp.Version)
	return nil
}

// pullFromCache reads env data from the local cache when the server is unreachable.
func pullFromCache(repoRoot string, opts PullOptions, printer *Printer) error {
	data, err := readCacheBlob(repoRoot)
	if err != nil {
		return fmt.Errorf("no cached data available (server unreachable)")
	}

	state, _ := readCacheState(repoRoot)

	envFile := opts.EnvFile
	if envFile == "" {
		envFile = state.EnvFile
	}
	if envFile == "" {
		envFile, _ = DetectEnvFile(repoRoot)
	}

	targetPath := filepath.Join(repoRoot, envFile)

	printer.Info("Warning: using cached data (version %d)", state.LastPulledVersion)

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(targetPath, data, 0o600); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}

	printer.Success("Pulled %s from cache (version %d)", envFile, state.LastPulledVersion)
	return nil
}

// checkGitignore warns if the env file is not listed in .gitignore.
func checkGitignore(repoRoot, envFile string, printer *Printer) {
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		printer.Info("Warning: no .gitignore found — consider adding %s to .gitignore", envFile)
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == envFile || line == "/"+envFile {
			return // Found it.
		}
	}

	printer.Info("Warning: %s is not in .gitignore — consider adding it", envFile)
}

// RunPush performs `dbx push`.
func RunPush(opts PushOptions, printer *Printer) error {
	repoRoot, err := GetRepoRoot(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Authenticate.
	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Resolve project.
	project, err := resolveProject(opts.Project, client, printer)
	if err != nil {
		return err
	}

	// Determine env file.
	envFile := resolveEnvFile(opts.EnvFile, project, repoRoot)
	targetPath := filepath.Join(repoRoot, envFile)

	// Read the file.
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", envFile, err)
	}

	// Validate: UTF-8.
	if !utf8.Valid(data) {
		return fmt.Errorf("%s is not valid UTF-8 — binary files are not supported", envFile)
	}

	// Validate: no null bytes (binary check).
	for _, b := range data {
		if b == 0 {
			return fmt.Errorf("%s contains null bytes — binary files are not supported", envFile)
		}
	}

	// Validate: size limit.
	if len(data) > maxEnvFileSize {
		return fmt.Errorf("%s exceeds 64KB limit (%d bytes)", envFile, len(data))
	}

	// Base64 encode.
	encoded := base64.StdEncoding.EncodeToString(data)

	// Read expected version from cache.
	state, _ := readCacheState(repoRoot)
	expectedVersion := state.LastPulledVersion

	// Push to server.
	pushResp, err := client.PushEnv(project.ID, encoded, expectedVersion, envFile)
	if err != nil {
		apiErr, ok := err.(*APIError)
		if ok && apiErr.StatusCode == 409 {
			if opts.Force {
				// Retry with version 0 for force push.
				printer.Info("Version conflict — force pushing...")
				pushResp, err = client.PushEnv(project.ID, encoded, 0, envFile)
				if err != nil {
					return fmt.Errorf("force push failed: %w", err)
				}
			} else {
				printer.Error("Version conflict: the server has a newer version.")
				printer.Error("Run `dbx pull` first to merge changes, or use `dbx push --force` to overwrite.")
				return fmt.Errorf("version conflict")
			}
		} else {
			return fmt.Errorf("pushing env vars: %w", err)
		}
	}

	// Update cache state.
	if err := writeCacheState(repoRoot, cacheState{
		LastPulledVersion: pushResp.Version,
		EnvFile:           envFile,
	}); err != nil {
		log.Printf("warning: failed to write cache state: %v", err)
	}

	// Also cache the blob we just pushed.
	if err := writeCacheBlob(repoRoot, data); err != nil {
		log.Printf("warning: failed to write cache blob: %v", err)
	}

	printer.Success("Pushed %s (version %d)", envFile, pushResp.Version)
	return nil
}

// RunDiff performs `dbx diff`.
func RunDiff(project string, envFile string, printer *Printer) error {
	repoRoot, err := GetRepoRoot(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Authenticate.
	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Resolve project.
	proj, err := resolveProject(project, client, printer)
	if err != nil {
		return err
	}

	// Determine env file.
	envFile = resolveEnvFile(envFile, proj, repoRoot)
	targetPath := filepath.Join(repoRoot, envFile)

	// Pull from server (don't write).
	envResp, err := client.PullEnv(proj.ID)
	if err != nil {
		return fmt.Errorf("pulling env vars: %w", err)
	}

	plaintext, err := base64.StdEncoding.DecodeString(envResp.Data)
	if err != nil {
		return fmt.Errorf("decoding env data: %w", err)
	}

	// Read local file.
	localData, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			printer.Info("Local file %s does not exist. Server version %d:", envFile, envResp.Version)
			// Show all server keys as added.
			serverKeys := parseEnvLines(string(plaintext))
			for key, val := range serverKeys {
				fmt.Fprintf(printer.Stdout, "+ %s=%s\n", key, maskValue(val))
			}
			printer.Info("%d keys on server, 0 locally", len(serverKeys))
			return nil
		}
		return fmt.Errorf("reading %s: %w", envFile, err)
	}

	// Compare.
	localStr := string(localData)
	serverStr := string(plaintext)

	if localStr == serverStr {
		printer.Success("No differences (version %d)", envResp.Version)
		return nil
	}

	localKeys := parseEnvLines(localStr)
	serverKeys := parseEnvLines(serverStr)

	var output []string
	addedCount, removedCount, changedCount := 0, 0, 0

	// Keys in local but not server (would be added by push).
	for key, val := range localKeys {
		if _, ok := serverKeys[key]; !ok {
			output = append(output, fmt.Sprintf("+ %s=%s  (local only)", key, maskValue(val)))
			addedCount++
		}
	}

	// Keys in server but not local (would be added by pull).
	for key, val := range serverKeys {
		if _, ok := localKeys[key]; !ok {
			output = append(output, fmt.Sprintf("- %s=%s  (server only)", key, maskValue(val)))
			removedCount++
		}
	}

	// Changed values.
	for key, localVal := range localKeys {
		if serverVal, ok := serverKeys[key]; ok && localVal != serverVal {
			output = append(output, fmt.Sprintf("~ %s  local=%s server=%s", key, maskValue(localVal), maskValue(serverVal)))
			changedCount++
		}
	}

	printer.Info("Diff: local %s vs server version %d", envFile, envResp.Version)
	for _, line := range output {
		fmt.Fprintln(printer.Stdout, line)
	}

	summary := []string{}
	if addedCount > 0 {
		summary = append(summary, strconv.Itoa(addedCount)+" local only")
	}
	if removedCount > 0 {
		summary = append(summary, strconv.Itoa(removedCount)+" server only")
	}
	if changedCount > 0 {
		summary = append(summary, strconv.Itoa(changedCount)+" changed")
	}
	printer.Info("%s", strings.Join(summary, ", "))

	return nil
}
