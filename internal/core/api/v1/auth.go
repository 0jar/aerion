package v1

import (
	"net/http"
)

// ClientConfigID identifies an OAuth client configuration (e.g., a specific
// Google Cloud project or Azure AD app registration). Each extension owns its
// own ClientConfigID, distinct from Mail's. See plan: each extension uses its
// own creds, with the same ClientConfigID potentially shared by future
// consolidation if combined-scope verification lands.
type ClientConfigID string

// AuthScope is a single OAuth scope an extension needs against a provider,
// paired with a human-readable reason shown to the user at consent time.
type AuthScope struct {
	Resource string `json:"resource"` // e.g., "https://www.googleapis.com/auth/calendar"
	Reason   string `json:"reason"`   // shown to user at consent
}

// IMAPClient and SMTPClient are interface{} here to avoid leaking go-imap/v2
// types into the public API surface. Concrete implementations type-assert to
// the appropriate client in the host package. We keep these typed as any so
// extensions can pass them to provider-specific code that imports the same
// client library directly (which is acceptable for first-party extensions
// living in the same Go module).
//
// If/when community extensions land, these become Aerion-defined facades.
type IMAPClient = any
type SMTPClient = any

// Auth is the only path extensions reach external services. They never see
// access tokens, refresh tokens, or passwords. Token refresh is transparent.
//
// Routing: the broker resolves the right ClientConfigID for the requested
// scopes (e.g., calendar scopes route to "google-extensions"). If the account
// lacks tokens covering those scopes under the target ClientConfigID, the
// broker returns ErrAdditionalConsentRequired and the host triggers an
// incremental-consent flow.
type Auth interface {
	// HTTPClient returns an *http.Client with bearer token injection and
	// transparent refresh-on-401. The extension calls the client normally.
	HTTPClient(accountID string, scopes []AuthScope) (*http.Client, error)

	// IMAPClient returns an authenticated IMAP client. Used for Sieve
	// (PUTSCRIPT), custom X-* commands, or any extension needing direct IMAP.
	IMAPClient(accountID string, requiredCaps []string) (IMAPClient, error)

	// SMTPClient returns an authenticated SMTP client. For outbound sends
	// not handled by the standard Compose API (e.g., delayed-send queues).
	SMTPClient(accountID string) (SMTPClient, error)
}
