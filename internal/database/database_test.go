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

// TestMigrationV32_LocalRecordIDsRewrittenToUUIDs verifies that migration 32
// transforms "local-<email>" record IDs into canonical UUIDv4s while keeping
// the contact_emails references intact. Simulates the upgrade path for a user
// who applied migration 31 (id format was "local-X@Y") and is now upgrading
// to the schema that uses UUIDs.
func TestMigrationV32_LocalRecordIDsRewrittenToUUIDs(t *testing.T) {
	db := openTestDB(t)

	// Seed legacy v31-shape data: a local record with the "local-<email>"
	// synthetic id. Delete the migration 32 marker so it re-applies and
	// rewrites this row.
	if _, err := db.Exec(`
		INSERT INTO contact_records (id, source, kind, fn)
		VALUES ('local-alice@example.com', 'local', 'collected', 'Alice')
	`); err != nil {
		t.Fatalf("seed contact_records: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO contact_emails (record_id, email, send_count, is_primary)
		VALUES ('local-alice@example.com', 'alice@example.com', 5, 1)
	`); err != nil {
		t.Fatalf("seed contact_emails: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM migrations WHERE version = 32`); err != nil {
		t.Fatalf("clear migration 32 marker: %v", err)
	}

	// Re-run migrations — migration 32 should rewrite the seeded local- id.
	if err := db.Migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	// Record id should now be a UUID (length 36, 4 dashes, hex elsewhere).
	var id string
	if err := db.QueryRow(`SELECT id FROM contact_records WHERE source = 'local'`).Scan(&id); err != nil {
		t.Fatalf("query rewritten id: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("id length = %d, want 36 (UUID)", len(id))
	}
	if id == "local-alice@example.com" {
		t.Errorf("id still has the legacy 'local-' shape: %q", id)
	}

	// contact_emails reference should point at the NEW id, not the old one.
	var refCount, emailCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contact_emails WHERE record_id = ?`, id).Scan(&refCount); err != nil {
		t.Fatalf("count refs to new id: %v", err)
	}
	if refCount != 1 {
		t.Errorf("contact_emails row pointing at new id: got %d, want 1", refCount)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contact_emails WHERE record_id = 'local-alice@example.com'`).Scan(&emailCount); err != nil {
		t.Fatalf("count orphan refs: %v", err)
	}
	if emailCount != 0 {
		t.Errorf("contact_emails still references old id: got %d orphan refs", emailCount)
	}

	// Email content + autocomplete metadata are unchanged.
	var email string
	var sendCount int
	if err := db.QueryRow(`SELECT email, send_count FROM contact_emails WHERE record_id = ?`, id).Scan(&email, &sendCount); err != nil {
		t.Fatalf("query preserved fields: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", email)
	}
	if sendCount != 5 {
		t.Errorf("send_count = %d, want 5 (preserved through migration)", sendCount)
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
