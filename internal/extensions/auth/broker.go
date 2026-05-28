package auth

import (
	"fmt"
	"net/http"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/oauth2"
)

// Broker is the concrete implementation of coreapi.Auth. It mediates between
// extensions and Aerion's credential store + OAuth manager. Extensions get
// pre-configured HTTP clients; tokens never leave the broker's boundary.
type Broker struct {
	credStore    *credentials.Store
	oauthManager *oauth2.Manager
}

// NewBroker constructs a Broker bound to the given credential store and
// OAuth manager. Both are required and must be non-nil.
func NewBroker(credStore *credentials.Store, oauthManager *oauth2.Manager) *Broker {
	return &Broker{
		credStore:    credStore,
		oauthManager: oauthManager,
	}
}

// HTTPClient returns an *http.Client that injects an OAuth2 bearer token on
// every request and refreshes it transparently on 401 responses.
//
// Routing: if the extension-scoped client config (google-extensions /
// microsoft-extensions) is provisioned, scope requests route to that client
// config. Otherwise they fall back to the mail client config (so extensions
// can be developed before the second OAuth project is in place).
//
// If the account lacks tokens covering all requested scopes under the resolved
// client config, returns *coreapi.ErrAdditionalConsentRequired. The host
// (NOT the extension) handles the consent flow and retries.
func (b *Broker) HTTPClient(accountID string, scopes []coreapi.AuthScope) (*http.Client, error) {
	// Discover the account's provider via its existing Mail tokens. Every
	// authenticated account has a Mail row (per migration v29 backfill).
	mailTokens, err := b.credStore.GetOAuthTokens(accountID)
	if err != nil {
		return nil, fmt.Errorf("auth broker: get account tokens: %w", err)
	}

	// Decide which client config the extension should use for this provider.
	_, extProvisioned := oauth2.ClientConfigForID(extConfigForProvider(mailTokens.Provider))
	clientConfigID := resolveClientConfigID(mailTokens.Provider, extProvisioned)
	if clientConfigID == "" {
		return nil, fmt.Errorf("auth broker: cannot resolve client config for provider %q", mailTokens.Provider)
	}

	// Check whether the account already has tokens under that client config
	// with sufficient scope coverage. If not, signal the host to run consent.
	existing, err := b.credStore.GetOAuthTokensForClientConfig(accountID, string(clientConfigID))
	if err == credentials.ErrCredentialNotFound {
		return nil, &coreapi.ErrAdditionalConsentRequired{
			AccountID:      accountID,
			ClientConfigID: clientConfigID,
			MissingScopes:  scopes,
		}
	}
	if err != nil {
		return nil, fmt.Errorf("auth broker: check tokens: %w", err)
	}

	if missing := missingScopes(existing.Scopes, scopes); len(missing) > 0 {
		return nil, &coreapi.ErrAdditionalConsentRequired{
			AccountID:      accountID,
			ClientConfigID: clientConfigID,
			MissingScopes:  missing,
		}
	}

	return &http.Client{
		Transport: &bearerRefreshTransport{
			base:           http.DefaultTransport,
			credStore:      b.credStore,
			oauthManager:   b.oauthManager,
			accountID:      accountID,
			clientConfigID: string(clientConfigID),
		},
	}, nil
}

// IMAPClient returns an authenticated IMAP client for the account. Phase 1
// scaffolds the interface; real IMAP wiring lands in Phase 2 when an
// extension needs it (Sieve, custom X-* commands, etc.). Mail itself
// continues to use the existing imap.Pool — it does NOT route through the
// broker.
func (b *Broker) IMAPClient(accountID string, requiredCaps []string) (coreapi.IMAPClient, error) {
	return nil, coreapi.ErrUnimplemented
}

// SMTPClient returns an authenticated SMTP client. Phase 1 stub; Mail uses
// the existing smtp.Client path directly. Extensions needing custom outbound
// (delayed-send queues, etc.) will wire this in Phase 2+.
func (b *Broker) SMTPClient(accountID string) (coreapi.SMTPClient, error) {
	return nil, coreapi.ErrUnimplemented
}
