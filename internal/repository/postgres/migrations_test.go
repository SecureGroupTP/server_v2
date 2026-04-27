package postgres_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChatRoomWelcomesConflictTargetRepairMigrationExists(t *testing.T) {
	root := repoRoot(t)
	migrationPath := filepath.Join(
		root,
		"db",
		"migrations",
		"V12__chat_room_welcomes_conflict_target.sql",
	)
	payload, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read repair migration: %v", err)
	}
	sql := strings.ToLower(string(payload))
	if !strings.Contains(sql, "create unique index if not exists chat_room_welcomes_pkey") {
		t.Fatalf("repair migration must create unique conflict target index, got:\n%s", payload)
	}
	if !strings.Contains(sql, "(room_id, target_user_public_key)") {
		t.Fatalf("repair migration must cover room_id and target_user_public_key, got:\n%s", payload)
	}
}

func repoRoot(t testing.TB) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test helper path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}
