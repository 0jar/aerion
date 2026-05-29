package contacts

import (
	"github.com/hkdb/aerion/internal/oauth2"
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

// contactsCredentialsProvider implements oauth2.CredentialsProvider for the
// Contacts extension's per-extension slots.
type contactsCredentialsProvider struct{}

func (contactsCredentialsProvider) Lookup(configID string) (oauth2.ClientCredentials, bool) {
	switch configID {
	case "google-contacts":
		if GoogleClientID == "" {
			return oauth2.ClientCredentials{}, false
		}
		return oauth2.ClientCredentials{
			ClientID:     GoogleClientID,
			ClientSecret: GoogleClientSecret,
		}, true
	case "microsoft-contacts":
		if MicrosoftClientID == "" {
			return oauth2.ClientCredentials{}, false
		}
		// Microsoft desktop apps omit client_secret (PKCE).
		return oauth2.ClientCredentials{
			ClientID: MicrosoftClientID,
		}, true
	default:
		return oauth2.ClientCredentials{}, false
	}
}

// init registers the Contacts extension's credentials provider so the global
// oauth2.ClientConfigForID resolution picks up google-contacts /
// microsoft-contacts. Runs once at package load — before any Extension.Register
// call, which is what we want.
func init() {
	oauth2.RegisterCredentialsProvider(contactsCredentialsProvider{})
}
