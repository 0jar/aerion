package backend

import (
	"path/filepath"
	"testing"

	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/database"
)

func setupAPI(t *testing.T) (*API, *contact.Store, *carddav.Store) {
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

	localStore := contact.NewStore(db.DB)
	carddavStore := carddav.NewStore(db.DB)

	// As of 2b.2.a (migration 31), the carddav-search bridge is no longer
	// needed: both local and carddav contacts live in the same unified tables,
	// and contact.Store.Search natively walks them. The legacy SetCardDAVSearchFunc
	// wiring was deleted from app.go + this test setup at the same time.

	return NewAPI(localStore, carddavStore), localStore, carddavStore
}

func TestAPI_SearchContacts_LocalOnly(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.AddOrUpdate("alice@example.com", "Alice"); err != nil {
		t.Fatalf("add local: %v", err)
	}
	if err := local.AddOrUpdate("bob@example.com", "Bob"); err != nil {
		t.Fatalf("add local: %v", err)
	}

	got, err := api.SearchContacts("alice", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].ID != "alice@example.com" {
		t.Fatalf("expected id=alice@example.com, got %q", got[0].ID)
	}
	if got[0].Name != "Alice" {
		t.Fatalf("expected name=Alice, got %q", got[0].Name)
	}
	if got[0].SourceID != "aerion" {
		t.Fatalf("expected source=aerion, got %q", got[0].SourceID)
	}
}

func TestAPI_GetContact_ByEmail(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.AddOrUpdate("alice@example.com", "Alice"); err != nil {
		t.Fatalf("add local: %v", err)
	}

	got, err := api.GetContact("alice@example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatalf("expected hit, got nil")
	}
	if got.Name != "Alice" || len(got.Emails) != 1 || got.Emails[0] != "alice@example.com" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestAPI_GetContact_ByEmail_Missing(t *testing.T) {
	api, _, _ := setupAPI(t)

	got, err := api.GetContact("nobody@example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing, got %+v", got)
	}
}

func TestAPI_GetContact_ByCardDAVID(t *testing.T) {
	api, _, carddavStore := setupAPI(t)

	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "Test", Type: carddav.SourceTypeCardDAV, URL: "https://example", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID:            "cdv-uuid-1",
		AddressbookID: ab.ID,
		Email:         "carol@example.com",
		DisplayName:   "Carol",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := api.GetContact("cdv-uuid-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil {
		t.Fatalf("expected hit, got nil")
	}
	if got.ID != "cdv-uuid-1" || got.Name != "Carol" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestAPI_ListContacts_Local(t *testing.T) {
	api, local, _ := setupAPI(t)

	for _, e := range []struct{ email, name string }{
		{"a@x", "A"},
		{"b@x", "B"},
		{"c@x", "C"},
	} {
		if err := local.AddOrUpdate(e.email, e.name); err != nil {
			t.Fatalf("add local: %v", err)
		}
	}

	got, err := api.ListContacts(coreapi.ContactFilter{SourceID: SourceIDLocal, Limit: 10})
	if err != nil {
		t.Fatalf("list local: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestAPI_ListContacts_CardDAVScoped(t *testing.T) {
	api, _, carddavStore := setupAPI(t)
	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	for i, name := range []string{"Alice", "Bob", "Carol"} {
		if err := carddavStore.UpsertContact(&carddav.Contact{
			ID:            "cdv-" + name,
			AddressbookID: ab.ID,
			Email:         name + "@example.com",
			DisplayName:   name,
		}); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	got, err := api.ListContacts(coreapi.ContactFilter{SourceID: src.ID, Limit: 10})
	if err != nil {
		t.Fatalf("list carddav: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// SourceID should be propagated to results
	for _, c := range got {
		if c.SourceID != src.ID {
			t.Fatalf("expected SourceID=%s, got %s", src.ID, c.SourceID)
		}
	}
}

func TestAPI_ListContacts_MergedAcrossSources(t *testing.T) {
	// "All" view (SourceID == "") merges local + CardDAV regardless of
	// whether a query is set. Empty query → match-all in each source.
	api, local, carddavStore := setupAPI(t)

	if err := local.AddOrUpdate("local-a@x", "Local A"); err != nil {
		t.Fatalf("add local: %v", err)
	}
	if err := local.AddOrUpdate("local-b@x", "Local B"); err != nil {
		t.Fatalf("add local: %v", err)
	}

	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID: "cdv-1", AddressbookID: ab.ID, Email: "carddav-c@x", DisplayName: "CardDAV C",
	}); err != nil {
		t.Fatalf("upsert carddav: %v", err)
	}

	got, err := api.ListContacts(coreapi.ContactFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list merged: %v", err)
	}
	// Expect all three: 2 local + 1 carddav.
	if len(got) != 3 {
		t.Fatalf("expected 3 merged results, got %d: %+v", len(got), got)
	}

	emails := map[string]bool{}
	for _, c := range got {
		if len(c.Emails) > 0 {
			emails[c.Emails[0]] = true
		}
	}
	for _, want := range []string{"local-a@x", "local-b@x", "carddav-c@x"} {
		if !emails[want] {
			t.Fatalf("expected merged result to include %q; got emails=%v", want, emails)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestAPI_UpdateContact_Local(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.AddOrUpdate("alice@example.com", "Alice Auto"); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	// Happy path: rename a local contact.
	if err := api.UpdateContact("alice@example.com", coreapi.ContactPatch{Name: strPtr("Alice Edit")}); err != nil {
		t.Fatalf("UpdateContact: %v", err)
	}

	got, err := api.GetContact("alice@example.com")
	if err != nil || got == nil {
		t.Fatalf("GetContact after update: got=%v err=%v", got, err)
	}
	if got.Name != "Alice Edit" {
		t.Fatalf("name after update: got %q, want %q", got.Name, "Alice Edit")
	}

	// Auto-collection on next send must NOT clobber the user edit.
	if err := local.AddOrUpdate("alice@example.com", "Alice Auto-2"); err != nil {
		t.Fatalf("AddOrUpdate after edit: %v", err)
	}
	got, _ = api.GetContact("alice@example.com")
	if got.Name != "Alice Edit" {
		t.Fatalf("auto-collection clobbered user edit: got %q, want %q", got.Name, "Alice Edit")
	}
}

func TestAPI_UpdateContact_NilPatchIsNoOp(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.AddOrUpdate("bob@example.com", "Bob Auto"); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	// Empty patch — name should stay as auto-collected.
	if err := api.UpdateContact("bob@example.com", coreapi.ContactPatch{}); err != nil {
		t.Fatalf("UpdateContact with empty patch: %v", err)
	}

	got, _ := api.GetContact("bob@example.com")
	if got == nil || got.Name != "Bob Auto" {
		t.Fatalf("empty patch should not have mutated; got %+v", got)
	}
}

func TestAPI_UpdateContact_CardDAVUnimplemented(t *testing.T) {
	api, _, carddavStore := setupAPI(t)

	// Seed a real CardDAV record so UpdateContact resolves it and rejects with
	// ErrUnimplemented at the source-type check (instead of returning nil for
	// a missing record).
	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID: "cdv-uuid-1", AddressbookID: ab.ID, Email: "carddav@example.com", DisplayName: "CD",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err = api.UpdateContact("cdv-uuid-1", coreapi.ContactPatch{Name: strPtr("ignored")})
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented for CardDAV id, got %v", err)
	}
}

func TestAPI_UpdateContact_EmptyIDRejected(t *testing.T) {
	api, _, _ := setupAPI(t)
	if err := api.UpdateContact("", coreapi.ContactPatch{Name: strPtr("x")}); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestAPI_DeleteContact_Local(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.AddOrUpdate("carol@example.com", "Carol"); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	if err := api.DeleteContact("carol@example.com"); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}

	got, err := api.GetContact("carol@example.com")
	if err != nil {
		t.Fatalf("GetContact after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestAPI_DeleteContact_CardDAVUnimplemented(t *testing.T) {
	api, _, carddavStore := setupAPI(t)

	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID: "cdv-uuid-1", AddressbookID: ab.ID, Email: "carddav@example.com", DisplayName: "CD",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err = api.DeleteContact("cdv-uuid-1")
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented for CardDAV id, got %v", err)
	}
}

func TestAPI_DeleteContact_EmptyIDRejected(t *testing.T) {
	api, _, _ := setupAPI(t)
	if err := api.DeleteContact(""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestAPI_SubscribeToContactEvents_Unimplemented(t *testing.T) {
	api, _, _ := setupAPI(t)
	_, _, err := api.SubscribeToContactEvents(nil)
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
}

func TestAPI_NilStores_GracefulDegradation(t *testing.T) {
	api := NewAPI(nil, nil)
	if got, err := api.SearchContacts("anything", 10); err != nil || got != nil {
		t.Fatalf("search with nil stores: got=%v err=%v", got, err)
	}
	if got, err := api.GetContact("a@x"); err != nil || got != nil {
		t.Fatalf("get with nil stores: got=%v err=%v", got, err)
	}
	if got, err := api.ListContacts(coreapi.ContactFilter{SourceID: SourceIDLocal}); err != nil || got != nil {
		t.Fatalf("list local with nil stores: got=%v err=%v", got, err)
	}
}

// TestAPI_GetContact_CardDAVByEmailFallback verifies the bug-fix path: when an
// email-shaped id surfaces from the "All" view (because the carddav-search
// bridge drops the UUID and fromLocal sets ID=email), GetContact must fall
// back to a CardDAV-by-email lookup when the local store has no entry for
// that email. Without this fallback the detail pane shows the empty
// placeholder for CardDAV-only contacts.
func TestAPI_GetContact_CardDAVByEmailFallback(t *testing.T) {
	api, _, carddavStore := setupAPI(t)

	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID: "cdv-only", AddressbookID: ab.ID, Email: "carddav-only@example.com", DisplayName: "CardDAV Only",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// The email is NOT in the local store. GetContact(email) must fall through
	// to carddavStore.GetContactByEmail.
	got, err := api.GetContact("carddav-only@example.com")
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	if got == nil {
		t.Fatal("expected CardDAV-by-email fallback to hit, got nil")
	}
	if got.Name != "CardDAV Only" {
		t.Errorf("expected name=%q, got %q", "CardDAV Only", got.Name)
	}
}

// TestAPI_GetContact_LocalPreferredOverCardDAV verifies precedence: when an
// email exists in BOTH stores, the local hit wins (so a user-edited name is
// preserved over the CardDAV display_name).
func TestAPI_GetContact_LocalPreferredOverCardDAV(t *testing.T) {
	api, local, carddavStore := setupAPI(t)

	if err := local.AddOrUpdate("dup@example.com", "Local Name"); err != nil {
		t.Fatalf("add local: %v", err)
	}

	src, err := carddavStore.CreateSource(&carddav.SourceConfig{
		Name: "S1", Type: carddav.SourceTypeCardDAV, URL: "https://x", Enabled: true, SyncInterval: 60,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ab, err := carddavStore.CreateAddressbook(src.ID, "/ab/", "ab", true)
	if err != nil {
		t.Fatalf("create addressbook: %v", err)
	}
	if err := carddavStore.UpsertContact(&carddav.Contact{
		ID: "cdv-1", AddressbookID: ab.ID, Email: "dup@example.com", DisplayName: "CardDAV Name",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := api.GetContact("dup@example.com")
	if err != nil || got == nil {
		t.Fatalf("GetContact: got=%v err=%v", got, err)
	}
	if got.Name != "Local Name" {
		t.Errorf("expected local entry to win, got name=%q", got.Name)
	}
}

func TestAPI_CreateContact_LocalManual(t *testing.T) {
	api, local, _ := setupAPI(t)

	id, err := api.CreateContact(coreapi.ContactCreateInput{
		Email: "new@example.com", Name: "Newly Added",
	})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if id != "new@example.com" {
		t.Errorf("returned id = %q, want %q", id, "new@example.com")
	}

	// Verify via direct store: kind=manual, send_count=0.
	c, err := local.Get("new@example.com")
	if err != nil || c == nil {
		t.Fatalf("Get after CreateContact: c=%v err=%v", c, err)
	}
	if c.Kind != "manual" {
		t.Errorf("Kind = %q, want manual", c.Kind)
	}
	if c.SendCount != 0 {
		t.Errorf("SendCount = %d, want 0", c.SendCount)
	}
}

func TestAPI_CreateContact_ExplicitSourceManual(t *testing.T) {
	api, _, _ := setupAPI(t)

	id, err := api.CreateContact(coreapi.ContactCreateInput{
		SourceID: SourceIDLocalManual,
		Email:    "explicit@example.com",
		Name:     "Explicit",
	})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if id != "explicit@example.com" {
		t.Errorf("id = %q", id)
	}
}

func TestAPI_CreateContact_NormalizesEmail(t *testing.T) {
	api, _, _ := setupAPI(t)
	id, err := api.CreateContact(coreapi.ContactCreateInput{
		Email: "  MIXED@Example.COM  ", Name: "Mixed",
	})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if id != "mixed@example.com" {
		t.Errorf("expected normalized id, got %q", id)
	}
}

func TestAPI_CreateContact_RejectsCollectedSource(t *testing.T) {
	api, _, _ := setupAPI(t)
	_, err := api.CreateContact(coreapi.ContactCreateInput{
		SourceID: SourceIDLocalCollected,
		Email:    "x@y.com",
	})
	if err == nil {
		t.Fatal("expected error rejecting Collected source, got nil")
	}
}

func TestAPI_CreateContact_RejectsEmptyEmail(t *testing.T) {
	api, _, _ := setupAPI(t)
	if _, err := api.CreateContact(coreapi.ContactCreateInput{}); err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestAPI_CreateContact_RejectsInvalidEmail(t *testing.T) {
	api, _, _ := setupAPI(t)
	if _, err := api.CreateContact(coreapi.ContactCreateInput{Email: "not-an-email"}); err == nil {
		t.Fatal("expected error for invalid email (no @)")
	}
}

func TestAPI_CreateContact_Conflict(t *testing.T) {
	api, _, _ := setupAPI(t)
	if _, err := api.CreateContact(coreapi.ContactCreateInput{Email: "dup@example.com", Name: "First"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := api.CreateContact(coreapi.ContactCreateInput{Email: "dup@example.com", Name: "Second"})
	if err == nil {
		t.Fatal("expected conflict error for duplicate email, got nil")
	}
	if err == coreapi.ErrUnimplemented {
		t.Fatalf("got ErrUnimplemented; conflict should surface ErrContactExists or similar, not Unimplemented")
	}
}

func TestAPI_CreateContact_UnknownSourceUnimplemented(t *testing.T) {
	api, _, _ := setupAPI(t)
	_, err := api.CreateContact(coreapi.ContactCreateInput{
		SourceID: "some-carddav-uuid",
		Email:    "x@y.com",
	})
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented for non-local source, got %v", err)
	}
}

func TestAPI_ListContacts_LocalManualSubsource(t *testing.T) {
	api, local, _ := setupAPI(t)

	// One manual + one collected.
	if err := local.Create("manual@example.com", "Manual"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := local.AddOrUpdate("collected@example.com", "Collected"); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	got, err := api.ListContacts(coreapi.ContactFilter{SourceID: SourceIDLocalManual, Limit: 10})
	if err != nil {
		t.Fatalf("ListContacts manual: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 manual contact, got %d: %+v", len(got), got)
	}
	if got[0].Emails[0] != "manual@example.com" {
		t.Errorf("got %s, want manual@example.com", got[0].Emails[0])
	}
}

func TestAPI_ListContacts_LocalCollectedSubsource(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.Create("manual@example.com", "Manual"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := local.AddOrUpdate("collected@example.com", "Collected"); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	got, err := api.ListContacts(coreapi.ContactFilter{SourceID: SourceIDLocalCollected, Limit: 10})
	if err != nil {
		t.Fatalf("ListContacts collected: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 collected contact, got %d: %+v", len(got), got)
	}
	if got[0].Emails[0] != "collected@example.com" {
		t.Errorf("got %s, want collected@example.com", got[0].Emails[0])
	}
}

func TestAPI_ListContacts_LocalParentReturnsBoth(t *testing.T) {
	api, local, _ := setupAPI(t)

	if err := local.Create("manual@example.com", "Manual"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := local.AddOrUpdate("collected@example.com", "Collected"); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	got, err := api.ListContacts(coreapi.ContactFilter{SourceID: SourceIDLocal, Limit: 10})
	if err != nil {
		t.Fatalf("ListContacts local-parent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 (manual + collected), got %d: %+v", len(got), got)
	}
}
