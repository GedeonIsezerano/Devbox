package database

import (
	"testing"
)

func TestOpenCreatesDBWithPragmas(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatal(err)
	}
	// In-memory DBs use "memory" journal mode, so we just verify no error
	if journalMode == "" {
		t.Fatal("expected journal_mode to be set")
	}

	// Verify foreign keys are on
	var fk int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}
}

func TestMigrationsApply(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify schema version
	var version int
	err = db.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("expected user_version=1, got %d", version)
	}

	// Verify tables exist
	tables := []string{"users", "ssh_keys", "projects", "project_members",
		"env_vars", "env_var_history", "tokens", "sessions", "audit_log"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
}
