package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// RunInit performs `dbx init [--name <name>]`.
func RunInit(name string, printer *Printer) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Server == "" {
		return fmt.Errorf("no server configured — run `dbx auth login` first")
	}

	// Get repo root.
	repoRoot, err := GetRepoRoot(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Get remote URL.
	remoteURL, err := GetRemoteURL(repoRoot)
	if err != nil {
		printer.Info("Warning: no git remote 'origin' found")
		remoteURL = ""
	}

	normalizedURL := ""
	if remoteURL != "" {
		normalizedURL = NormalizeRemoteURL(remoteURL)
	}

	// Detect env file.
	envFile, projectType := DetectEnvFile(repoRoot)

	// Default project name from directory basename if not given.
	if name == "" {
		name = filepath.Base(repoRoot)
	}

	if projectType != "" {
		printer.Info("Detected %s project", projectType)
	}
	printer.Info("Env file: %s", envFile)

	// Authenticate.
	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Create project on server.
	proj, err := client.CreateProject(name, normalizedURL, envFile)
	if err != nil {
		return fmt.Errorf("creating project: %w", err)
	}

	printer.Success("Project %q created (ID: %s)", proj.Name, proj.ID)
	if proj.RemoteURL != "" {
		printer.Info("Remote: %s", proj.RemoteURL)
	}
	printer.Info("Env file: %s", proj.EnvFile)
	printer.Info("")
	printer.Info("Next steps:")
	printer.Info("  dbx push    — push your %s to the server", envFile)
	printer.Info("  dbx pull    — pull env vars from the server")

	return nil
}

// RunProjectList performs `dbx project list`.
func RunProjectList(printer *Printer) error {
	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	projects, err := client.ListProjects()
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}

	if printer.JSON {
		return printer.Data(projects)
	}

	if len(projects) == 0 {
		printer.Info("No projects found. Run `dbx init` to create one.")
		return nil
	}

	w := tabwriter.NewWriter(printer.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tENV FILE\tREMOTE")
	for _, p := range projects {
		remote := p.RemoteURL
		if remote == "" {
			remote = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.ID, p.EnvFile, remote)
	}
	return w.Flush()
}

// RunProjectDelete performs `dbx project delete <name>`.
func RunProjectDelete(name string, yes bool, printer *Printer) error {
	if name == "" {
		return fmt.Errorf("project name is required")
	}

	// Prompt for confirmation unless --yes or CI.
	if !yes && !printer.IsCI {
		fmt.Fprintf(printer.Stderr, "Delete project %q? This cannot be undone. [y/N] ", name)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			printer.Info("Cancelled")
			return nil
		}
	}

	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Find project by name to get the ID.
	projects, err := client.ListProjects()
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}

	var projectID string
	for _, p := range projects {
		if p.Name == name {
			projectID = p.ID
			break
		}
	}

	if projectID == "" {
		return fmt.Errorf("project %q not found", name)
	}

	if err := client.DeleteProject(projectID); err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}

	printer.Success("Project %q deleted", name)
	return nil
}
