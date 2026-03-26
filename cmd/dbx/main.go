package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/user/devbox/internal/cli"
)

var version = "0.0.1-dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		verbose bool
		quiet   bool
	)

	rootCmd := &cobra.Command{
		Use:   "dbx",
		Short: "Environment variable management for dev environments",
		Long: `dbx manages environment variables for development teams.
Push and pull .env files to a shared server, with SSH-key authentication
and versioned storage.

Getting started:
  1. dbx auth login --server https://your-server.com
  2. cd your-project && dbx init
  3. dbx push
  4. dbx pull  (from any clone or worktree)

For cloud environments, set DEVBOX_TOKEN instead of using SSH keys.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-error output")

	// Helper to create a printer with the global flags applied.
	makePrinter := func() *cli.Printer {
		p := cli.NewPrinter()
		p.Verbose = verbose
		p.Quiet = quiet
		return p
	}

	// Helper to create a JSON-capable printer for --format json commands.
	makePrinterWithFormat := func(format *string) *cli.Printer {
		p := makePrinter()
		if format != nil && *format == "json" {
			p.JSON = true
		}
		return p
	}

	// --- init ---
	var initName string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project in the current repository",
		Long: `Initialize a project in the current repository by reading the git remote,
detecting the project type (Node.js, Python, Go, etc.), and registering
the project on the server. The project name defaults to the directory name
but can be overridden with --name.`,
		Example: `  dbx init                        # use git remote and directory name
  dbx init --name myapp           # override project name`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunInit(initName, makePrinter())
		},
	}
	initCmd.Flags().StringVar(&initName, "name", "", "project name (defaults to directory name)")

	// --- push ---
	var pushForce bool
	var pushProject, pushEnvFile string
	pushCmd := &cobra.Command{
		Use:   "push",
		Short: "Push local env file to the server",
		Long: `Push the local env file to the server. The project and env file are
auto-detected from the git remote and project type. Uses optimistic
locking to prevent overwriting changes — if someone else pushed since
your last pull, you'll get a version conflict (409). Use --force to
overwrite the server version.`,
		Example: `  dbx push                        # push env file for current project
  dbx push --force                # overwrite server version on conflict
  dbx push --project myapp        # specify project explicitly`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunPush(cli.PushOptions{
				Project: pushProject,
				EnvFile: pushEnvFile,
				Force:   pushForce,
			}, makePrinter())
		},
	}
	pushCmd.Flags().BoolVar(&pushForce, "force", false, "force push, overwriting server version")
	pushCmd.Flags().StringVar(&pushProject, "project", "", "project name")
	pushCmd.Flags().StringVar(&pushEnvFile, "env-file", "", "env file name")

	// --- pull ---
	var pullForce, pullDiff, pullBackup, pullCached bool
	var pullProject, pullEnvFile string
	pullCmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull env file from the server",
		Long: `Pull the env file from the server and write it to disk. The project is
auto-detected from the git remote, and the filename is chosen based on the
project type (.env.local for Node/TS, .env for Python/Go/Rust). If the local
file already exists and differs, you'll be prompted before overwriting unless
--force is set. Use --diff to preview changes without writing. The --cached
flag falls back to a locally cached copy when the server is unreachable.`,
		Example: `  dbx pull                        # auto-detect project and env file
  dbx pull --force                # overwrite without prompting
  dbx pull --diff                 # preview changes without writing
  dbx pull --backup               # save old file as .env.local.backup
  dbx pull --cached               # use cache if server unreachable
  dbx pull --project myapp        # specify project explicitly
  dbx pull --env-file .env        # override auto-detected filename`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunPull(cli.PullOptions{
				Project: pullProject,
				EnvFile: pullEnvFile,
				Force:   pullForce,
				Diff:    pullDiff,
				Backup:  pullBackup,
				Cached:  pullCached,
			}, makePrinter())
		},
	}
	pullCmd.Flags().BoolVar(&pullForce, "force", false, "overwrite local file without prompting")
	pullCmd.Flags().BoolVar(&pullDiff, "diff", false, "show diff without writing")
	pullCmd.Flags().BoolVar(&pullBackup, "backup", false, "create a backup before overwriting")
	pullCmd.Flags().BoolVar(&pullCached, "cached", false, "use cached data if server is unreachable")
	pullCmd.Flags().StringVar(&pullProject, "project", "", "project name")
	pullCmd.Flags().StringVar(&pullEnvFile, "env-file", "", "env file name")

	// --- diff ---
	var diffProject, diffEnvFile string
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between local and server env files",
		Long: `Show a unified diff between the local env file and the version stored on
the server. No files are modified. Useful for reviewing what changed before
pushing or pulling.`,
		Example: `  dbx diff                        # compare local vs server
  dbx diff --project myapp        # for a specific project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunDiff(diffProject, diffEnvFile, makePrinter())
		},
	}
	diffCmd.Flags().StringVar(&diffProject, "project", "", "project name")
	diffCmd.Flags().StringVar(&diffEnvFile, "env-file", "", "env file name")

	// --- auth ---
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}

	var authLoginServer string
	authLoginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a devbox server",
		Long: `Authenticate with a devbox server using your SSH key. The CLI discovers
keys in this order: ssh-agent, then ~/.ssh/id_ed25519, id_ed25519_sk,
id_ecdsa, id_rsa. On first login, your public key is registered with the
server (the first user becomes admin). The server URL is saved to
~/.config/dbx/config.toml for subsequent commands.`,
		Example: `  dbx auth login --server https://dbx.example.com
  dbx auth login --server http://localhost:8443`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunAuthLogin(authLoginServer, makePrinter())
		},
	}
	authLoginCmd.Flags().StringVar(&authLoginServer, "server", "", "server URL")
	authLoginCmd.MarkFlagRequired("server")

	var authStatusFormat string
	authStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := makePrinterWithFormat(&authStatusFormat)
			return cli.RunAuthStatus(p)
		},
	}
	authStatusCmd.Flags().StringVar(&authStatusFormat, "format", "", "output format (json)")

	authLogoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out from the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunAuthLogout(makePrinter())
		},
	}

	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd)

	// --- token ---
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens",
	}

	var tokenCreateName, tokenCreateType, tokenCreateScope, tokenCreateTTL, tokenCreateProjectID string
	tokenCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token",
		Long: `Create a new API token. PAT (Personal Access Token) tokens are long-lived
and suitable for cloud environments like Claude Code or Codex. Provision
tokens are single-use and time-limited, ideal for CI or one-off access.
Default TTL for PATs is 90 days (max 365 days). Provision tokens require
--project-id and --ttl.`,
		Example: `  dbx token create --name "laptop"                            # PAT, 90d default
  dbx token create --name "ci" --type provision --ttl 1h      # single-use
  dbx token create --name "cloud" --scope project:read        # read-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunTokenCreate(tokenCreateName, tokenCreateType, tokenCreateScope, tokenCreateTTL, tokenCreateProjectID, makePrinter())
		},
	}
	tokenCreateCmd.Flags().StringVar(&tokenCreateName, "name", "", "token name")
	tokenCreateCmd.Flags().StringVar(&tokenCreateType, "type", "pat", "token type (pat or provision)")
	tokenCreateCmd.Flags().StringVar(&tokenCreateScope, "scope", "", "token scope")
	tokenCreateCmd.Flags().StringVar(&tokenCreateTTL, "ttl", "", "token time-to-live (e.g., 30d, 1y)")
	tokenCreateCmd.Flags().StringVar(&tokenCreateProjectID, "project-id", "", "project ID (required for provision tokens)")
	tokenCreateCmd.MarkFlagRequired("name")

	var tokenListFormat string
	tokenListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := makePrinterWithFormat(&tokenListFormat)
			return cli.RunTokenList(p)
		},
	}
	tokenListCmd.Flags().StringVar(&tokenListFormat, "format", "", "output format (json)")

	tokenRevokeCmd := &cobra.Command{
		Use:   "revoke <name>",
		Short: "Revoke a token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunTokenRevoke(args[0], makePrinter())
		},
	}

	tokenCmd.AddCommand(tokenCreateCmd, tokenListCmd, tokenRevokeCmd)

	// --- project ---
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	var projectListFormat string
	projectListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := makePrinterWithFormat(&projectListFormat)
			return cli.RunProjectList(p)
		},
	}
	projectListCmd.Flags().StringVar(&projectListFormat, "format", "", "output format (json)")

	var projectDeleteYes bool
	projectDeleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunProjectDelete(args[0], projectDeleteYes, makePrinter())
		},
	}
	projectDeleteCmd.Flags().BoolVar(&projectDeleteYes, "yes", false, "skip confirmation prompt")

	projectCmd.AddCommand(projectListCmd, projectDeleteCmd)

	// --- whoami ---
	var whoamiFormat string
	whoamiCmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current authenticated identity",
		Long: `Show the current authenticated identity, including the username, auth method
(SSH or token), and server URL. Use --format json for machine-readable output.`,
		Example: `  dbx whoami                      # show identity
  dbx whoami --format json        # machine-readable output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := makePrinterWithFormat(&whoamiFormat)
			return cli.RunWhoami(p)
		},
	}
	whoamiCmd.Flags().StringVar(&whoamiFormat, "format", "", "output format (json)")

	// --- completion ---
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for dbx.

To load completions:

Bash:
  $ source <(dbx completion bash)

Zsh:
  $ source <(dbx completion zsh)

Fish:
  $ dbx completion fish | source`,
		Args:              cobra.ExactArgs(1),
		ValidArgs:         []string{"bash", "zsh", "fish"},
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			}
			return fmt.Errorf("unsupported shell: %s", args[0])
		},
	}

	// Add all commands to root.
	rootCmd.AddCommand(
		initCmd,
		pushCmd,
		pullCmd,
		diffCmd,
		authCmd,
		tokenCmd,
		projectCmd,
		whoamiCmd,
		completionCmd,
	)

	// Execute and map errors to exit codes.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return exitCode(err)
	}
	return 0
}

// exitCode maps errors to process exit codes.
//   - 0: success
//   - 1: general error
//   - 2: authentication error
//   - 3: not found
func exitCode(err error) int {
	var apiErr *cli.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return 2
		case 404:
			return 3
		}
	}

	// Check for common auth-related error messages from ResolveAuth.
	msg := err.Error()
	if strings.Contains(msg, "no server configured") ||
		strings.Contains(msg, "no SSH key found") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "token authentication failed") ||
		strings.Contains(msg, "verification failed") {
		return 2
	}

	return 1
}
