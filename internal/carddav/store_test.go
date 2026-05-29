package carddav

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/database"
)

func openCardDAVTestDB(t *testing.T) *database.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedSource inserts a source + one addressbook + n contacts. Returns the
// source ID and addressbook ID so tests can reference them.
func seedSource(t *testing.T, s *Store, sourceID, abID string, enabled, abEnabled bool, contacts []*Contact) {
	t.Helper()
	_, err := s.db.Exec(`
		INSERT INTO contact_sources (id, name, type, url, enabled, sync_interval, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, sourceID, "src-"+sourceID, SourceTypeCardDAV, "https://example/", boolToInt(enabled), 60, time.Now())
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO contact_source_addressbooks (id, source_id, path, name, enabled)
		VALUES (?, ?, ?, ?, ?)
	`, abID, sourceID, "/"+abID+"/", "ab-"+abID, boolToInt(abEnabled))
	if err != nil {
		t.Fatalf("insert addressbook: %v", err)
	}
	for _, c := range contacts {
		c.AddressbookID = abID
		if err := s.UpsertContact(c); err != nil {
			t.Fatalf("upsert contact: %v", err)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestListContactsPaged_Basic(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
		{ID: "c2", Email: "bob@example.com", DisplayName: "Bob"},
		{ID: "c3", Email: "carol@example.com", DisplayName: "Carol"},
	})

	got, err := s.ListContactsPaged("src1", "", 0, 10)
	if err != nil {
		t.Fatalf("ListContactsPaged: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// Ordered by display_name ASC
	if got[0].DisplayName != "Alice" || got[1].DisplayName != "Bob" || got[2].DisplayName != "Carol" {
		t.Fatalf("ordering wrong: %v %v %v", got[0].DisplayName, got[1].DisplayName, got[2].DisplayName)
	}
}

func TestListContactsPaged_Paging(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "a@x", DisplayName: "A"},
		{ID: "c2", Email: "b@x", DisplayName: "B"},
		{ID: "c3", Email: "c@x", DisplayName: "C"},
		{ID: "c4", Email: "d@x", DisplayName: "D"},
		{ID: "c5", Email: "e@x", DisplayName: "E"},
	})

	page1, err := s.ListContactsPaged("src1", "", 0, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].DisplayName != "A" || page1[1].DisplayName != "B" {
		t.Fatalf("page 1 mismatch: %v", page1)
	}

	page2, err := s.ListContactsPaged("src1", "", 2, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].DisplayName != "C" || page2[1].DisplayName != "D" {
		t.Fatalf("page 2 mismatch: %v", page2)
	}

	page3, err := s.ListContactsPaged("src1", "", 4, 2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 1 || page3[0].DisplayName != "E" {
		t.Fatalf("page 3 mismatch: %v", page3)
	}
}

func TestListContactsPaged_FiltersDisabledSource(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", false, true, []*Contact{
		{ID: "c1", Email: "a@x", DisplayName: "A"},
	})

	got, err := s.ListContactsPaged("src1", "", 0, 10)
	if err != nil {
		t.Fatalf("ListContactsPaged: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 (disabled source), got %d", len(got))
	}
}

func TestListContactsPaged_FiltersDisabledAddressbook(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, false, []*Contact{
		{ID: "c1", Email: "a@x", DisplayName: "A"},
	})

	got, err := s.ListContactsPaged("src1", "", 0, 10)
	if err != nil {
		t.Fatalf("ListContactsPaged: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 (disabled addressbook), got %d", len(got))
	}
}

func TestListContactsPaged_ScopesToSource(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "a@x", DisplayName: "A"},
	})
	seedSource(t, s, "src2", "ab2", true, true, []*Contact{
		{ID: "c2", Email: "b@x", DisplayName: "B"},
	})

	got, err := s.ListContactsPaged("src1", "", 0, 10)
	if err != nil {
		t.Fatalf("ListContactsPaged src1: %v", err)
	}
	if len(got) != 1 || got[0].DisplayName != "A" {
		t.Fatalf("src1 scope mismatch: %v", got)
	}

	got, err = s.ListContactsPaged("src2", "", 0, 10)
	if err != nil {
		t.Fatalf("ListContactsPaged src2: %v", err)
	}
	if len(got) != 1 || got[0].DisplayName != "B" {
		t.Fatalf("src2 scope mismatch: %v", got)
	}
}

func TestListContactsPaged_QueryFilter(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice Smith"},
		{ID: "c2", Email: "bob@example.com", DisplayName: "Bob Jones"},
		{ID: "c3", Email: "carol@other.com", DisplayName: "Carol Smith"},
	})

	// Match by name fragment, case-insensitive.
	got, err := s.ListContactsPaged("src1", "smith", 0, 10)
	if err != nil {
		t.Fatalf("query smith: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 (Alice + Carol Smith), got %d", len(got))
	}

	// Match by email fragment.
	got, err = s.ListContactsPaged("src1", "example.com", 0, 10)
	if err != nil {
		t.Fatalf("query example.com: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 (@example.com), got %d", len(got))
	}

	// Empty query returns all.
	got, err = s.ListContactsPaged("src1", "", 0, 10)
	if err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 (all), got %d", len(got))
	}

	// No matches → empty (not error).
	got, err = s.ListContactsPaged("src1", "nobody", 0, 10)
	if err != nil {
		t.Fatalf("no-match query: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(got))
	}
}

func TestListContactsPaged_DefaultsLimit(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	contacts := make([]*Contact, 60)
	for i := 0; i < 60; i++ {
		contacts[i] = &Contact{
			ID:          string(rune('A'+i/26)) + string(rune('a'+i%26)),
			Email:       string(rune('a'+i%26)) + "@x",
			DisplayName: string(rune('A'+i%26)),
		}
	}
	seedSource(t, s, "src1", "ab1", true, true, contacts)

	// limit <= 0 must default to 50
	got, err := s.ListContactsPaged("src1", "", 0, 0)
	if err != nil {
		t.Fatalf("ListContactsPaged: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("expected default limit 50, got %d", len(got))
	}
}

func TestGetContactByEmail_Match(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
		{ID: "c2", Email: "bob@example.com", DisplayName: "Bob"},
	})

	got, err := s.GetContactByEmail("alice@example.com")
	if err != nil {
		t.Fatalf("GetContactByEmail: %v", err)
	}
	if got == nil {
		t.Fatal("expected match, got nil")
	}
	if got.ID != "c1" || got.DisplayName != "Alice" {
		t.Errorf("got %+v, want c1/Alice", got)
	}
}

func TestGetContactByEmail_CaseInsensitive(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
	})

	got, err := s.GetContactByEmail("ALICE@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetContactByEmail: %v", err)
	}
	if got == nil || got.ID != "c1" {
		t.Errorf("case-insensitive lookup failed: got %+v", got)
	}
}

func TestGetContactByEmail_NoMatch(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
	})

	got, err := s.GetContactByEmail("nobody@example.com")
	if err != nil {
		t.Fatalf("GetContactByEmail: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for no match, got %+v", got)
	}
}

func TestGetContactByEmail_FiltersDisabledSource(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	// Source is DISABLED — the contact should be invisible to GetContactByEmail
	// (matches SearchContacts visibility).
	seedSource(t, s, "src1", "ab1", false, true, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
	})

	got, err := s.GetContactByEmail("alice@example.com")
	if err != nil {
		t.Fatalf("GetContactByEmail: %v", err)
	}
	if got != nil {
		t.Errorf("disabled source should not surface: got %+v", got)
	}
}

func TestGetContactByEmail_FiltersDisabledAddressbook(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	seedSource(t, s, "src1", "ab1", true, false, []*Contact{
		{ID: "c1", Email: "alice@example.com", DisplayName: "Alice"},
	})

	got, err := s.GetContactByEmail("alice@example.com")
	if err != nil {
		t.Fatalf("GetContactByEmail: %v", err)
	}
	if got != nil {
		t.Errorf("disabled addressbook should not surface: got %+v", got)
	}
}

func TestGetContactByEmail_EmptyArg(t *testing.T) {
	db := openCardDAVTestDB(t)
	s := NewStore(db.DB)

	got, err := s.GetContactByEmail("")
	if err != nil {
		t.Fatalf("GetContactByEmail(\"\"): %v", err)
	}
	if got != nil {
		t.Errorf("empty email should return nil, got %+v", got)
	}
}
