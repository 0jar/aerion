package oauth2

import (
	"fmt"
	"strings"
	"sync"
)

// ClientCredentials is the OAuth2 client_id + secret pair for one client
// configuration. Each first-party extension owns its own client configuration
// so it can be verified, deployed, and revoked independently from Mail.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// CredentialsProvider is the source-of-credentials interface that Aerion core
// and each extension implement. ClientConfigForID walks the registered chain
// at lookup time; first provider that knows the requested configID wins.
//
// Each extension owns its OWN credential injection at build time (per-extension
// .env / shim + a small creds.go in the extension package that registers a
// CredentialsProvider during Extension.Register()). Aerion core compiles in
// only its own *-mail creds via the built-in core provider.
type CredentialsProvider interface {
	// Lookup returns the credentials for the given client config id, or
	// (zero, false) if this provider doesn't know that id (or the value is
	// not yet provisioned, e.g., empty build-time var).
	Lookup(configID string) (ClientCredentials, bool)
}

// UserOverrideLookup is an optional pluggable hook for user-supplied creds
// (Settings → OAuth Credentials). If non-nil, it's checked BEFORE the provider
// chain — user values always win. Set during App.Startup by the credentials
// store package; can be nil during tests or if user-overrides are unused.
var UserOverrideLookup func(configID string) (ClientCredentials, bool)

var (
	providersMu sync.RWMutex
	providers   []CredentialsProvider
)

// RegisterCredentialsProvider appends a provider to the resolution chain.
// Safe to call from package init() functions or from Extension.Register().
// Order matters: providers are queried in registration order, first-hit wins.
// Aerion core registers itself early (init); extensions register at their
// Register() time, after core. Result: core's *-mail slots always resolve
// before any extension's slots — but since slot names don't collide between
// core and extensions, the order is purely a performance hint.
func RegisterCredentialsProvider(p CredentialsProvider) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = append(providers, p)
}

// ClientConfigForID returns the credentials registered for the given client
// config id. Resolution order:
//
//  1. User override from credentials.Store (Settings UI override), if any
//  2. Walk registered CredentialsProviders in registration order
//  3. (zero, false) if nothing matches
//
// Known config ids today: 'google-mail' / 'microsoft-mail' (Aerion core),
// 'google-contacts' / 'microsoft-contacts' (Contacts extension), and
// (future) 'google-calendar' / 'microsoft-calendar' (Calendar extension).
// Extension provider registration happens in each extension's package; if
// the extension hasn't been compiled in (or its credentials aren't yet
// provisioned), its slots return (zero, false) gracefully.
func ClientConfigForID(id string) (ClientCredentials, bool) {
	if UserOverrideLookup != nil {
		if creds, ok := UserOverrideLookup(id); ok {
			return creds, true
		}
	}
	providersMu.RLock()
	defer providersMu.RUnlock()
	for _, p := range providers {
		if creds, ok := p.Lookup(id); ok {
			return creds, true
		}
	}
	return ClientCredentials{}, false
}

// ClientConfigIDForProvider maps a provider name (as used by the existing
// GetProvider API and stored in the oauth_tokens.provider column) to its
// default mail-flavored client_config_id. Used by the credentials store to
// route legacy queries to the right client config.
//
//	"google", "google-contacts"       → "google-mail"
//	"microsoft", "microsoft-contacts" → "microsoft-mail"
func ClientConfigIDForProvider(name string) string {
	switch name {
	case "google", "google-contacts":
		return "google-mail"
	case "microsoft", "microsoft-contacts":
		return "microsoft-mail"
	default:
		return ""
	}
}

// GetProviderForClientConfig returns the OAuth2 provider configuration for
// the given client_config_id. The returned ProviderConfig carries the scopes
// and URLs appropriate to the provider (Google vs Microsoft) along with the
// client credentials registered for that specific client config.
//
// Used by the Auth Broker (internal/extensions/auth) when an extension needs
// to reach external services. Scopes in the returned config are the default
// for the underlying provider; callers may override Scopes for extension-
// specific scope subsets (e.g., calendar-only).
func GetProviderForClientConfig(clientConfigID string) (ProviderConfig, error) {
	creds, ok := ClientConfigForID(clientConfigID)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("client config not configured: %s", clientConfigID)
	}
	switch {
	case strings.HasPrefix(clientConfigID, "google-"):
		cfg := GoogleProvider()
		cfg.ClientID = creds.ClientID
		cfg.ClientSecret = creds.ClientSecret
		return cfg, nil
	case strings.HasPrefix(clientConfigID, "microsoft-"):
		cfg := MicrosoftProvider()
		cfg.ClientID = creds.ClientID
		cfg.ClientSecret = creds.ClientSecret
		return cfg, nil
	default:
		return ProviderConfig{}, fmt.Errorf("cannot determine provider for client config: %s", clientConfigID)
	}
}
