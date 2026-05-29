package oauth2

// coreProvider is Aerion core's CredentialsProvider. It knows about the
// `*-mail` client configs only. Extensions register their own providers in
// their own packages (e.g., extensions/contacts/creds.go).
//
// Registered automatically at package init so any ClientConfigForID call
// after the package loads can resolve `google-mail` / `microsoft-mail`.
type coreProvider struct{}

func (coreProvider) Lookup(configID string) (ClientCredentials, bool) {
	switch configID {
	case "google-mail":
		if GoogleClientID == "" {
			return ClientCredentials{}, false
		}
		return ClientCredentials{ClientID: GoogleClientID, ClientSecret: GoogleClientSecret}, true
	case "microsoft-mail":
		if MicrosoftClientID == "" {
			return ClientCredentials{}, false
		}
		// Microsoft desktop apps omit the client secret (uses PKCE).
		return ClientCredentials{ClientID: MicrosoftClientID, ClientSecret: ""}, true
	default:
		return ClientCredentials{}, false
	}
}

func init() {
	RegisterCredentialsProvider(coreProvider{})
}
