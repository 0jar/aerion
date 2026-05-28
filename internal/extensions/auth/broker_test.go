package auth

import (
	"path/filepath"
	"testing"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/oauth2"
)

// newTestBroker spins up a temp DB + credentials store + OAuth manager for
// broker integration tests. Real OAuth refresh isn't exercised here (that
// requires an httptest.Server stub of provider token endpoints) — the focus
// is the broker's pre-call decisions: scope coverage, client-config routing,
// ErrAdditionalConsentRequired paths.
func newTestBroker(t *testing.T) (*Broker, *credentials.Store, *database.DB) {
	t.Helper()
	tmp := t.TempDir()
	db, err := database.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	credStore, err := credentials.NewStore(db.DB, tmp)
	if err != nil {
		t.Fatalf("credentials.NewStore: %v", err)
	}

	mgr := oauth2.NewManager()
	return NewBroker(credStore, mgr), credStore, db
}

func insertTestAccount(t *testing.T, db *database.DB, id string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		VALUES (?, 'Test', ?, 'imap.example.com', 'smtp.example.com', ?)
	`, id, id+"@example.com", id+"@example.com")
	if err != nil {
		t.Fatalf("insert account %s: %v", id, err)
	}
}

func TestBrokerHTTPClient_AccountNotFound(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.HTTPClient("nonexistent-account", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent account, got nil")
	}
}

func TestBrokerHTTPClient_ScopesUncovered_ReturnsConsentRequired(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-a")

	// Account has Mail tokens with mail-only scopes — does NOT cover Calendar.
	err := credStore.SetOAuthTokens("acct-a", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "access-token-mail",
		RefreshToken: "refresh-token-mail",
		Scopes:       []string{"https://mail.google.com/"},
	})
	if err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	_, err = broker.HTTPClient("acct-a", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err == nil {
		t.Fatal("expected ErrAdditionalConsentRequired, got nil")
	}
	consentErr, ok := err.(*coreapi.ErrAdditionalConsentRequired)
	if !ok {
		t.Fatalf("expected *ErrAdditionalConsentRequired, got %T: %v", err, err)
	}
	if consentErr.AccountID != "acct-a" {
		t.Errorf("AccountID: got %q, want %q", consentErr.AccountID, "acct-a")
	}
	if len(consentErr.MissingScopes) != 1 {
		t.Errorf("MissingScopes len: got %d, want 1", len(consentErr.MissingScopes))
	}
}

func TestBrokerHTTPClient_ScopesCovered_ReturnsClient(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-b")

	// Account has tokens covering both Mail AND Calendar scopes under the
	// mail config. (Phase 1 without extension config provisioned: the broker
	// falls back to the mail config, so a mail-config token with both scopes
	// represents the "all scopes granted" case.)
	err := credStore.SetOAuthTokens("acct-b", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "access-token-combined",
		RefreshToken: "refresh-token-combined",
		Scopes: []string{
			"https://mail.google.com/",
			"https://www.googleapis.com/auth/calendar",
		},
	})
	if err != nil {
		t.Fatalf("set combined tokens: %v", err)
	}

	client, err := broker.HTTPClient("acct-b", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err != nil {
		t.Fatalf("HTTPClient returned error for covered scopes: %v", err)
	}
	if client == nil {
		t.Fatal("HTTPClient returned nil client")
	}
	if client.Transport == nil {
		t.Fatal("client has no transport")
	}
}

func TestBrokerIMAPClient_Unimplemented(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.IMAPClient("any", nil)
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
}

func TestBrokerSMTPClient_Unimplemented(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.SMTPClient("any")
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
}
