package postgrestest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	image    = "postgres:16-alpine"
	database = "app"
	username = "postgres"
	password = "postgres"
)

type Store interface {
	DB() *sql.DB
}

func DSN(t testing.TB) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var container *tcpostgres.PostgresContainer
	container, err := tcpostgres.Run(
		ctx,
		image,
		tcpostgres.WithDatabase(database),
		tcpostgres.WithUsername(username),
		tcpostgres.WithPassword(password),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres test container: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(shutdownCtx)); err != nil {
			t.Fatalf("terminate postgres test container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres container dsn: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres migration connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres migration connection: %v", err)
	}

	applyMigrations(t, db)
	return dsn
}

func applyMigrations(t testing.TB, db *sql.DB) {
	t.Helper()

	migrations, err := filepath.Glob(filepath.Join(repoRoot(t), "db", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("find migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no postgres migrations found")
	}
	sort.Strings(migrations)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, migration := range migrations {
		payload, err := os.ReadFile(migration)
		if err != nil {
			t.Fatalf("read migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(payload)); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(migration), err)
		}
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

func CleanupTables(t testing.TB, store Store) {
	t.Helper()
	_, err := store.DB().Exec(`
	TRUNCATE ban_statuses, chat_invitations, chat_member_permissions, direct_rooms, chat_members, chat_room_states, chat_rooms, friends, friend_requests, key_packages, device_push_tokens, user_events, event_subscriptions, auth_sessions, profiles RESTART IDENTITY CASCADE
`)
	if err != nil {
		t.Fatalf("cleanup postgres tables: %v", err)
	}
}

func AuthTablesOnlyCleanup(t testing.TB, store Store) {
	t.Helper()
	_, err := store.DB().Exec(`
TRUNCATE ban_statuses, user_events, event_subscriptions, auth_sessions, profiles RESTART IDENTITY CASCADE
`)
	if err != nil {
		t.Fatalf("cleanup postgres auth tables: %v", err)
	}
}
