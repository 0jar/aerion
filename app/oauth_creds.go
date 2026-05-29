package app

import (
	"fmt"

	"github.com/hkdb/aerion/internal/oauth2"
)

// OAuthCredsStatus is the metadata returned by GetOAuthCredsStatus. Secret
// values themselves NEVER leave the credentials store via this surface — only
// presence flags + a short fingerprint of the client_id for visual
// confirmation in the Settings UI.
type OAuthCredsStatus struct {
	// ConfigID is the slot identifier (e.g., "google-mail", "google-contacts").
	ConfigID string `json:"configId"`
	// HasUserOverride is true when the user has supplied their own creds for
	// this slot via Settings → OAuth Credentials.
	HasUserOverride bool `json:"hasUserOverride"`
	// HasShipped is true when shipped/built-in creds for this slot are
	// populated (the build-time vars are non-empty).
	HasShipped bool `json:"hasShipped"`
	// ClientIDFingerprint is the last 4 characters of the currently-active
	// client_id (whichever wins resolution — user override beats shipped).
	// Empty when no creds exist at all. Used by the UI for visual
	// confirmation that the saved value is what the user expects.
	ClientIDFingerprint string `json:"clientIdFingerprint"`
}

// GetOAuthCredsStatus reports whether user-supplied AND/OR shipped creds are
// present for the given client config id. Never exposes the secret values.
//
// Wails-bound. Called by Settings → Accounts → OAuth Credentials section AND
// by each extension's settings dialog (when checking its own slots).
func (a *App) GetOAuthCredsStatus(configID string) (OAuthCredsStatus, error) {
	status := OAuthCredsStatus{ConfigID: configID}

	// Has user override?
	if a.credStore != nil {
		status.HasUserOverride = a.credStore.HasUserClientCreds(configID)
	}

	// Has shipped creds? Temporarily unset the UserOverrideLookup so we can
	// query only the registered providers' shipped values.
	saved := oauth2.UserOverrideLookup
	oauth2.UserOverrideLookup = nil
	_, hasShipped := oauth2.ClientConfigForID(configID)
	oauth2.UserOverrideLookup = saved
	status.HasShipped = hasShipped

	// Compute fingerprint from the WINNING set of creds (user override if any,
	// else shipped) — re-query with the override lookup restored.
	activeCreds, ok := oauth2.ClientConfigForID(configID)
	status.ClientIDFingerprint = fingerprintClientID(ok, activeCreds.ClientID)

	return status, nil
}

func fingerprintClientID(found bool, id string) string {
	if !found || id == "" {
		return ""
	}
	if len(id) > 4 {
		return "…" + id[len(id)-4:]
	}
	return id
}

// SetOAuthCreds saves user-supplied OAuth client credentials for the given
// config id. Overrides any shipped/built-in values for that slot.
//
// Wails-bound.
func (a *App) SetOAuthCreds(configID, clientID, clientSecret string) error {
	if a.credStore == nil {
		return fmt.Errorf("credential store not initialized")
	}
	return a.credStore.SetUserClientCreds(configID, clientID, clientSecret)
}

// ClearOAuthCreds removes a user-supplied override for the given config id,
// reverting that slot to its shipped value (or empty if none was shipped).
//
// Wails-bound.
func (a *App) ClearOAuthCreds(configID string) error {
	if a.credStore == nil {
		return fmt.Errorf("credential store not initialized")
	}
	return a.credStore.ClearUserClientCreds(configID)
}

// CopyOAuthCreds reads the user-supplied creds for fromConfigID and writes
// them as the user-supplied creds for toConfigID. Secret values never cross
// the Wails boundary — the copy happens entirely server-side via credStore.
//
// Wails-bound. Used by the "Copy from another slot…" picker in the OAuth
// Credentials UI when the user wants to reuse one verified project's creds
// across multiple extension slots.
func (a *App) CopyOAuthCreds(fromConfigID, toConfigID string) error {
	if a.credStore == nil {
		return fmt.Errorf("credential store not initialized")
	}
	if fromConfigID == "" || toConfigID == "" {
		return fmt.Errorf("source and destination config ids are both required")
	}
	if fromConfigID == toConfigID {
		return nil
	}

	clientID, clientSecret, ok, err := a.credStore.GetUserClientCreds(fromConfigID)
	if err != nil {
		return fmt.Errorf("read source creds: %w", err)
	}
	if !ok {
		return fmt.Errorf("no user-supplied creds found for %q", fromConfigID)
	}
	return a.credStore.SetUserClientCreds(toConfigID, clientID, clientSecret)
}
