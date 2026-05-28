package oauth2

import (
	"fmt"
	"strings"
)

// ClientCredentials is the OAuth2 client_id + secret pair for one client
// configuration. Each first-party extension owns its own client configuration
// so it can be verified, deployed, and revoked independently from Mail.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// ClientConfigForID returns the credentials registered for the given client
// config id, or (zero, false) if the config is unknown or not yet provisioned
// (e.g., 'google-extensions' before the second Google project is set up).
//
// Known ids:
//   - "google-mail"          — current verified Mail-scoped Google project
//   - "google-extensions"    — extension-scoped Google project (Calendar/Contacts)
//   - "microsoft-mail"       — current Mail-scoped Azure AD registration
//   - "microsoft-extensions" — extension-scoped Azure AD registration
//
// Build-time vars backing these ids live in config.go.
func ClientConfigForID(id string) (ClientCredentials, bool) {
	switch id {
	case "google-mail":
		if GoogleClientID == "" {
			return ClientCredentials{}, false
		}
		return ClientCredentials{ClientID: GoogleClientID, ClientSecret: GoogleClientSecret}, true
	case "google-extensions":
		if GoogleExtClientID == "" {
			return ClientCredentials{}, false
		}
		return ClientCredentials{ClientID: GoogleExtClientID, ClientSecret: GoogleExtClientSecret}, true
	case "microsoft-mail":
		if MicrosoftClientID == "" {
			return ClientCredentials{}, false
		}
		return ClientCredentials{ClientID: MicrosoftClientID, ClientSecret: ""}, true
	case "microsoft-extensions":
		if MicrosoftExtClientID == "" {
			return ClientCredentials{}, false
		}
		return ClientCredentials{ClientID: MicrosoftExtClientID, ClientSecret: ""}, true
	default:
		return ClientCredentials{}, false
	}
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
