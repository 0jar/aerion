package contacts

import (
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// Build-time OAuth credentials for the Contacts extension. These are injected
// at build time via ldflags from a per-extension source (typically
// extensions/contacts/.env or a separate aerion-creds-contacts shim binary).
// Empty values are valid — when a slot is empty, the Auth Broker returns
// ErrAdditionalConsentRequired and the host's incremental-consent UI fires.
//
// Each first-party extension owns its OWN credential injection. Aerion core
// compiles in only its own google-mail / microsoft-mail creds. See
// `docs/EXTENSIONS.md` "Write Capability" section for the build-time wiring
// pattern community extensions should follow.
var (
	// GoogleClientID is the OAuth2 client ID for the Contacts extension's
	// Google Cloud project. Carries the contacts WRITE scope; the contacts
	// READ scope continues to ride mail OAuth via the manifest's
	// first_party_uses_core_for_scopes list.
	GoogleClientID string

	// GoogleClientSecret pairs with GoogleClientID.
	GoogleClientSecret string

	// MicrosoftClientID is the OAuth2 client ID for the Contacts extension's
	// Azure AD app registration. May be the same value as Aerion core's
	// MicrosoftClientID if the user has added the Contacts.ReadWrite scope to
	// the existing mail registration (Microsoft permits scope-adds without
	// re-review).
	MicrosoftClientID string
)

// OAuthClients returns the per-extension OAuth client configurations the
// Contacts extension contributes. The host calls this at startup and
// registers each entry into the global oauth2.ClientConfigForID resolver
// chain. Entries with empty ClientID are ignored — extensions can declare
// all their slots unconditionally and rely on build-time ldflags to fill in
// only the ones they have credentials for.
//
// This declarative pattern replaces the previous package-init RegisterCredentials
// call. By going through coreapi, the extension no longer needs to import
// `internal/oauth2` — that's the host's plumbing, not the extension's.
func OAuthClients() []coreapi.OAuthProviderRegistration {
	return []coreapi.OAuthProviderRegistration{
		{
			ConfigID:     "google-contacts",
			ClientID:     GoogleClientID,
			ClientSecret: GoogleClientSecret,
		},
		{
			// Microsoft desktop apps with PKCE omit the client secret.
			ConfigID: "microsoft-contacts",
			ClientID: MicrosoftClientID,
		},
	}
}
