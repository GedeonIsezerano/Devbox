package database

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestPushEnvVars(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	blob := []byte("KEY=value\nSECRET=hunter2")
	version, err := PushEnvVars(db, project.ID, "default", blob, 0, user.ID)
	if err != nil {
		t.Fatalf("PushEnvVars failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}
}

func TestPushEnvVarsSecondPush(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	blob1 := []byte("KEY=value1")
	version1, err := PushEnvVars(db, project.ID, "default", blob1, 0, user.ID)
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}
	if version1 != 1 {
		t.Fatalf("expected version 1, got %d", version1)
	}

	blob2 := []byte("KEY=value2")
	version2, err := PushEnvVars(db, project.ID, "default", blob2, 1, user.ID)
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}
	if version2 != 2 {
		t.Fatalf("expected version 2, got %d", version2)
	}
}

func TestPushEnvVarsOptimisticLock(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// First push: version 0 -> 1
	_, err = PushEnvVars(db, project.ID, "default", []byte("v1"), 0, user.ID)
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	// Second push: version 1 -> 2
	_, err = PushEnvVars(db, project.ID, "default", []byte("v2"), 1, user.ID)
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}

	// Conflicting push: expects version 1 but current is 2
	_, err = PushEnvVars(db, project.ID, "default", []byte("v3-conflict"), 1, user.ID)
	if err == nil {
		t.Fatal("expected version conflict error, got nil")
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}
}

func TestPullEnvVars(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	blob := []byte("KEY=value\nSECRET=hunter2")
	_, err = PushEnvVars(db, project.ID, "default", blob, 0, user.ID)
	if err != nil {
		t.Fatalf("PushEnvVars failed: %v", err)
	}

	data, err := PullEnvVars(db, project.ID, "default")
	if err != nil {
		t.Fatalf("PullEnvVars failed: %v", err)
	}
	if !bytes.Equal(data.Blob, blob) {
		t.Fatalf("expected blob %q, got %q", blob, data.Blob)
	}
	if data.Version != 1 {
		t.Fatalf("expected version 1, got %d", data.Version)
	}
}

func TestPullEnvVarsNotFound(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = PullEnvVars(db, project.ID, "default")
	if err == nil {
		t.Fatal("expected error when no env vars exist, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestGetEnvVersion(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = PushEnvVars(db, project.ID, "default", []byte("KEY=val"), 0, user.ID)
	if err != nil {
		t.Fatalf("PushEnvVars failed: %v", err)
	}

	version, err := GetEnvVersion(db, project.ID, "default")
	if err != nil {
		t.Fatalf("GetEnvVersion failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}
}

func TestEnvVarHistory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = PushEnvVars(db, project.ID, "default", []byte("KEY=val"), 0, user.ID)
	if err != nil {
		t.Fatalf("PushEnvVars failed: %v", err)
	}

	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM env_var_history WHERE project_id = ? AND environment = ?",
		project.ID, "default",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query history count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 history entry, got %d", count)
	}

	// Verify the history entry has the correct version
	var histVersion int
	err = db.QueryRow(
		"SELECT version FROM env_var_history WHERE project_id = ? AND environment = ? ORDER BY version DESC LIMIT 1",
		project.ID, "default",
	).Scan(&histVersion)
	if err != nil {
		t.Fatalf("query history version: %v", err)
	}
	if histVersion != 1 {
		t.Fatalf("expected history version 1, got %d", histVersion)
	}
}

func TestEnvVarHistoryPruning(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Push 12 times
	for i := 0; i < 12; i++ {
		expectedVersion := i // 0 for first push, then 1, 2, 3, ...
		blob := []byte(fmt.Sprintf("KEY=value%d", i+1))
		_, err := PushEnvVars(db, project.ID, "default", blob, expectedVersion, user.ID)
		if err != nil {
			t.Fatalf("push %d failed: %v", i+1, err)
		}
	}

	// Verify only 10 history rows remain
	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM env_var_history WHERE project_id = ? AND environment = ?",
		project.ID, "default",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query history count: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 history entries after pruning, got %d", count)
	}

	// Verify the oldest remaining version is 3 (versions 1 and 2 should be pruned)
	var minVersion int
	err = db.QueryRow(
		"SELECT MIN(version) FROM env_var_history WHERE project_id = ? AND environment = ?",
		project.ID, "default",
	).Scan(&minVersion)
	if err != nil {
		t.Fatalf("query min version: %v", err)
	}
	if minVersion != 3 {
		t.Fatalf("expected oldest version to be 3, got %d", minVersion)
	}

	// Verify the newest version is 12
	var maxVersion int
	err = db.QueryRow(
		"SELECT MAX(version) FROM env_var_history WHERE project_id = ? AND environment = ?",
		project.ID, "default",
	).Scan(&maxVersion)
	if err != nil {
		t.Fatalf("query max version: %v", err)
	}
	if maxVersion != 12 {
		t.Fatalf("expected newest version to be 12, got %d", maxVersion)
	}
}

func TestPushEnvVarsForceOverwrite(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProject(db, "testproj", "github.com/user/repo", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// First push: version 0 -> 1.
	blob1 := []byte("KEY=value1")
	v1, err := PushEnvVars(db, project.ID, "default", blob1, 0, user.ID)
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("expected version 1, got %d", v1)
	}

	// Second push with version 1 -> 2.
	blob2 := []byte("KEY=value2")
	v2, err := PushEnvVars(db, project.ID, "default", blob2, 1, user.ID)
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("expected version 2, got %d", v2)
	}

	// Force push with version 0 when row already exists — should overwrite.
	blob3 := []byte("KEY=forced")
	v3, err := PushEnvVars(db, project.ID, "default", blob3, 0, user.ID)
	if err != nil {
		t.Fatalf("force push failed: %v", err)
	}
	if v3 != 3 {
		t.Fatalf("expected version 3 after force push, got %d", v3)
	}

	// Verify the content was overwritten.
	data, err := PullEnvVars(db, project.ID, "default")
	if err != nil {
		t.Fatalf("pull after force push failed: %v", err)
	}
	if !bytes.Equal(data.Blob, blob3) {
		t.Fatalf("expected blob %q, got %q", blob3, data.Blob)
	}
	if data.Version != 3 {
		t.Fatalf("expected version 3, got %d", data.Version)
	}
}
