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

	// Wire the carddav search bridge so contact.Store.Search merges local +
	// carddav results, matching how app.go Startup wires production. Without
	// this, "All" view returns local-only and several tests don't reflect
	// real behavior.
	localStore.SetCardDAVSearchFunc(func(query string, limit int) ([]*contact.Contact, error) {
		cdContacts, err := carddavStore.SearchContacts(query, limit)
		if err != nil {
			return nil, err
		}
		out := make([]*contact.Contact, 0, len(cdContacts))
		for _, c := range cdContacts {
			out = append(out, &contact.Contact{
				Email:       c.Email,
				DisplayName: c.DisplayName,
				Source:      "carddav",
			})
		}
		return out, nil
	})

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
