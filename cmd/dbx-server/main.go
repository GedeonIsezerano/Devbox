package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
	"github.com/user/devbox/internal/server"
)

var version = "0.0.1-dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "dbx-server",
		Short:   "Devbox secrets server",
		Version: version,
	}

	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(emergencyRevokeAllCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	var (
		dataPath   string
		ageKeyPath string
		listenAddr string
		tlsCert    string
		tlsKey     string
		noTLS      bool
		allowRoot  bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the devbox server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(dataPath, ageKeyPath, listenAddr, tlsCert, tlsKey, noTLS, allowRoot)
		},
	}

	cmd.Flags().StringVar(&dataPath, "data", "./dbx.db", "path to SQLite database file")
	cmd.Flags().StringVar(&ageKeyPath, "age-key", "./age.key", "path to age identity file")
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:8443", "listen address")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate path")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS private key path")
	cmd.Flags().BoolVar(&noTLS, "no-tls", false, "run without TLS (for local dev / reverse proxy)")
	cmd.Flags().BoolVar(&allowRoot, "allow-root", false, "allow running as root")

	return cmd
}

func runServe(dataPath, ageKeyPath, listenAddr, tlsCert, tlsKey string, noTLS, allowRoot bool) error {
	// 1. Refuse to start as root unless --allow-root.
	if os.Getuid() == 0 && !allowRoot {
		return fmt.Errorf("refusing to run as root; use --allow-root to override")
	}

	// 2. Ensure database directory exists and open database.
	if dir := filepath.Dir(dataPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	db, err := database.Open(dataPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	log.Printf("database opened: %s", dataPath)

	// 3. Load or generate age identity.
	var encryptor crypto.Encryptor
	if _, err := os.Stat(ageKeyPath); err == nil {
		// File exists — load it.
		enc, err := crypto.NewAgeEncryptorFromFile(ageKeyPath)
		if err != nil {
			return fmt.Errorf("load age identity: %w", err)
		}
		encryptor = enc
		log.Printf("age identity loaded from %s", ageKeyPath)
	} else if key := os.Getenv("DBX_AGE_KEY"); key != "" {
		// Env var set — use it.
		enc, err := crypto.NewAgeEncryptorFromEnv()
		if err != nil {
			return fmt.Errorf("load age identity from env: %w", err)
		}
		encryptor = enc
		log.Print("age identity loaded from DBX_AGE_KEY environment variable")
	} else {
		// Generate new identity.
		if err := crypto.GenerateAgeIdentity(ageKeyPath); err != nil {
			return fmt.Errorf("generate age identity: %w", err)
		}
		enc, err := crypto.NewAgeEncryptorFromFile(ageKeyPath)
		if err != nil {
			return fmt.Errorf("load generated age identity: %w", err)
		}
		encryptor = enc
		log.Printf("age identity generated and saved to %s", ageKeyPath)
	}

	// 4. Create nonce store.
	nonces := crypto.NewNonceStore(60 * time.Second)
	defer nonces.Stop()

	// 5. Create server.
	srv := server.NewServer(server.Config{
		ListenAddr: listenAddr,
		DB:         db,
		Encryptor:  encryptor,
		Nonces:     nonces,
	})

	// 6. Handle graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)

	go func() {
		if noTLS {
			log.Printf("listening on http://%s (no TLS)", listenAddr)
			errCh <- srv.ListenAndServe()
		} else {
			if tlsCert == "" || tlsKey == "" {
				errCh <- fmt.Errorf("--tls-cert and --tls-key are required unless --no-tls is set")
				return
			}
			srv.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			log.Printf("listening on https://%s", listenAddr)
			errCh <- srv.ListenAndServeTLS(tlsCert, tlsKey)
		}
	}()

	select {
	case <-ctx.Done():
		log.Print("shutdown signal received, draining connections...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		log.Print("server stopped gracefully")
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

func backupCmd() *cobra.Command {
	var (
		dataPath string
		output   string
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(dataPath, output)
		},
	}

	cmd.Flags().StringVar(&dataPath, "data", "./dbx.db", "source database path")
	cmd.Flags().StringVar(&output, "output", "", "output path for backup")
	cmd.MarkFlagRequired("output")

	return cmd
}

func runBackup(dataPath, output string) error {
	db, err := database.Open(dataPath)
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	defer db.Close()

	// Ensure output directory exists.
	if dir := filepath.Dir(output); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	// Use VACUUM INTO for an online, consistent backup.
	_, err = db.Exec(fmt.Sprintf("VACUUM INTO '%s'", output))
	if err != nil {
		return fmt.Errorf("backup database: %w", err)
	}

	fmt.Printf("Backup created: %s\n", output)
	return nil
}

func emergencyRevokeAllCmd() *cobra.Command {
	var dataPath string

	cmd := &cobra.Command{
		Use:   "emergency-revoke-all",
		Short: "Revoke all sessions and tokens (emergency use only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEmergencyRevokeAll(dataPath)
		},
	}

	cmd.Flags().StringVar(&dataPath, "data", "./dbx.db", "database path")

	return cmd
}

func runEmergencyRevokeAll(dataPath string) error {
	db, err := database.Open(dataPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	sessionCount, err := database.DeleteAllSessions(db)
	if err != nil {
		return fmt.Errorf("delete all sessions: %w", err)
	}

	tokenCount, err := database.DeleteAllTokens(db)
	if err != nil {
		return fmt.Errorf("delete all tokens: %w", err)
	}

	// Log audit event.
	if err := database.LogEvent(db, database.AuditEntry{
		Action:   "emergency.revoke_all",
		Metadata: fmt.Sprintf(`{"sessions_revoked":%d,"tokens_revoked":%d}`, sessionCount, tokenCount),
	}); err != nil {
		return fmt.Errorf("log audit event: %w", err)
	}

	fmt.Printf("Revoked %d sessions and %d tokens\n", sessionCount, tokenCount)
	return nil
}
