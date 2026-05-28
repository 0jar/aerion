package database

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if db == nil {
		t.Fatal("Open() returned nil DB")
	}
}

func TestMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
}

func TestUpdateIdleConns(t *testing.T) {
	db := openTestDB(t)

	tests := []struct {
		name        string
		numAccounts int
	}{
		{"zero accounts", 0},
		{"three accounts", 3},
		{"ten accounts", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify no panic
			db.UpdateIdleConns(tt.numAccounts)
		})
	}
}

func TestCheckpoint(t *testing.T) {
	db := openTestDB(t)

	if err := db.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
}

// TestMigrationV29_OAuthCompositeKey verifies the Phase 1 extension-system
// migration: oauth_tokens now uses composite PK (account_id, client_config_id)
// so a single account can hold separate token rows for Mail vs extension-
// scoped OAuth clients.
func TestMigrationV29_OAuthCompositeKey(t *testing.T) {
	db := openTestDB(t)

	// Insert a test account row (oauth_tokens.account_id FK to accounts.id).
	// Schema defaults handle most columns; only NOT NULL non-default fields are explicit.
	if _, err := db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		VALUES ('acct-1', 'Test', 'user@example.com', 'imap.example.com', 'smtp.example.com', 'user@example.com')
	`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Insert mail-config token row
	if _, err := db.Exec(`
		INSERT INTO oauth_tokens (account_id, client_config_id, provider, expires_at, scopes)
		VALUES ('acct-1', 'google-mail', 'google', CURRENT_TIMESTAMP, '[]')
	`); err != nil {
		t.Fatalf("insert mail token row: %v", err)
	}

	// Insert extension-config token row for same account — should succeed
	if _, err := db.Exec(`
		INSERT INTO oauth_tokens (account_id, client_config_id, provider, expires_at, scopes)
		VALUES ('acct-1', 'google-extensions', 'google', CURRENT_TIMESTAMP, '[]')
	`); err != nil {
		t.Fatalf("insert extension token row failed (composite PK should allow it): %v", err)
	}

	// Verify both rows exist
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM oauth_tokens WHERE account_id = 'acct-1'`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 token rows for account, got %d", count)
	}

	// Duplicate (account_id, client_config_id) must violate the composite PK
	if _, err := db.Exec(`
		INSERT INTO oauth_tokens (account_id, client_config_id, provider, expires_at, scopes)
		VALUES ('acct-1', 'google-mail', 'google', CURRENT_TIMESTAMP, '[]')
	`); err == nil {
		t.Fatal("expected composite PK conflict on duplicate (account_id, client_config_id), got no error")
	}
}

func TestPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if got := db.Path(); got != path {
		t.Errorf("Path() = %q, want %q", got, path)
	}
}
