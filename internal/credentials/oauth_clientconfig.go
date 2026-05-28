package credentials

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	gokeyring "github.com/zalando/go-keyring"
)

// =============================================================================
// Multi-client-config OAuth methods (Phase 1+ extension system)
//
// These methods extend the existing single-config OAuth helpers with explicit
// client_config_id selection so a single account can hold tokens under multiple
// OAuth client configurations (one for Mail, one for Calendar, etc.).
//
// Storage:
//   - DB metadata: oauth_tokens has composite PK (account_id, client_config_id).
//   - Keyring tokens: keyed as "<accountID>:<clientConfigID>:<kind>". The legacy
//     "<accountID>:<kind>" format is read as a fallback for back-compat.
//   - Encrypted-DB fallback: only Mail-config tokens use the accounts table
//     fallback columns. Non-Mail configs require keyring availability.
// =============================================================================

// SetOAuthTokensForClientConfig stores OAuth tokens for an account under a
// specific client_config_id. New code (extension Auth Broker, OAuth flow for
// extension-scope grants) calls this instead of SetOAuthTokens.
func (s *Store) SetOAuthTokensForClientConfig(accountID, clientConfigID string, tokens *OAuthTokens) error {
	if tokens == nil {
		return fmt.Errorf("tokens cannot be nil")
	}
	if clientConfigID == "" {
		return fmt.Errorf("clientConfigID cannot be empty")
	}

	if err := s.setOAuthAccessTokenForClientConfig(accountID, clientConfigID, tokens.AccessToken); err != nil {
		return fmt.Errorf("failed to store access token: %w", err)
	}
	if err := s.setOAuthRefreshTokenForClientConfig(accountID, clientConfigID, tokens.RefreshToken); err != nil {
		return fmt.Errorf("failed to store refresh token: %w", err)
	}

	scopesJSON, err := json.Marshal(tokens.Scopes)
	if err != nil {
		return fmt.Errorf("failed to marshal scopes: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO oauth_tokens (account_id, client_config_id, provider, expires_at, scopes, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(account_id, client_config_id) DO UPDATE SET
			provider = excluded.provider,
			expires_at = excluded.expires_at,
			scopes = excluded.scopes,
			updated_at = CURRENT_TIMESTAMP
	`, accountID, clientConfigID, tokens.Provider, tokens.ExpiresAt, string(scopesJSON))
	if err != nil {
		return fmt.Errorf("failed to store OAuth metadata: %w", err)
	}

	s.log.Debug().
		Str("account_id", accountID).
		Str("client_config_id", clientConfigID).
		Str("provider", tokens.Provider).
		Time("expires_at", tokens.ExpiresAt).
		Msg("OAuth tokens stored (client-config-aware)")
	return nil
}

// GetOAuthTokensForClientConfig retrieves OAuth tokens for the account under
// the given client_config_id, returning ErrCredentialNotFound when no row
// exists for that pair.
func (s *Store) GetOAuthTokensForClientConfig(accountID, clientConfigID string) (*OAuthTokens, error) {
	var provider string
	var expiresAt sql.NullTime
	var scopesJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT provider, expires_at, scopes
		FROM oauth_tokens
		WHERE account_id = ? AND client_config_id = ?
	`, accountID, clientConfigID).Scan(&provider, &expiresAt, &scopesJSON)
	if err == sql.ErrNoRows {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query OAuth metadata: %w", err)
	}

	accessToken, err := s.getOAuthAccessTokenForClientConfig(accountID, clientConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}
	refreshToken, err := s.getOAuthRefreshTokenForClientConfig(accountID, clientConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get refresh token: %w", err)
	}

	var scopes []string
	if scopesJSON.Valid && scopesJSON.String != "" {
		if err := json.Unmarshal([]byte(scopesJSON.String), &scopes); err != nil {
			s.log.Warn().Err(err).Msg("Failed to parse OAuth scopes, using empty list")
			scopes = []string{}
		}
	}

	tokens := &OAuthTokens{
		Provider:     provider,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Scopes:       scopes,
	}
	if expiresAt.Valid {
		tokens.ExpiresAt = expiresAt.Time
	}
	return tokens, nil
}

// DeleteOAuthTokensForClientConfig removes the (account_id, client_config_id)
// token row and its keyring entries. Does not touch other rows for the same
// account (e.g., deleting Calendar tokens leaves Mail tokens intact).
func (s *Store) DeleteOAuthTokensForClientConfig(accountID, clientConfigID string) error {
	if s.keyringEnabled {
		_ = gokeyring.Delete(serviceName, accountID+":"+clientConfigID+":access_token")
		_ = gokeyring.Delete(serviceName, accountID+":"+clientConfigID+":refresh_token")
	}

	_, err := s.db.Exec(
		"DELETE FROM oauth_tokens WHERE account_id = ? AND client_config_id = ?",
		accountID, clientConfigID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth metadata: %w", err)
	}

	s.log.Debug().
		Str("account_id", accountID).
		Str("client_config_id", clientConfigID).
		Msg("OAuth tokens deleted (client-config-aware)")
	return nil
}

// UpdateOAuthAccessTokenForClientConfig updates the access token and expiry
// for a specific (account, client_config) pair after a token refresh.
func (s *Store) UpdateOAuthAccessTokenForClientConfig(accountID, clientConfigID, accessToken string, expiresAt time.Time) error {
	if err := s.setOAuthAccessTokenForClientConfig(accountID, clientConfigID, accessToken); err != nil {
		return fmt.Errorf("failed to store access token: %w", err)
	}

	_, err := s.db.Exec(`
		UPDATE oauth_tokens
		SET expires_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE account_id = ? AND client_config_id = ?
	`, expiresAt, accountID, clientConfigID)
	if err != nil {
		return fmt.Errorf("failed to update OAuth expiry: %w", err)
	}

	s.log.Debug().
		Str("account_id", accountID).
		Str("client_config_id", clientConfigID).
		Time("expires_at", expiresAt).
		Msg("OAuth access token updated (client-config-aware)")
	return nil
}

// HasOAuthTokensForClientConfig returns true if the (account, client_config)
// pair has a token row.
func (s *Store) HasOAuthTokensForClientConfig(accountID, clientConfigID string) bool {
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM oauth_tokens WHERE account_id = ? AND client_config_id = ?",
		accountID, clientConfigID,
	).Scan(&count)
	return err == nil && count > 0
}

// -----------------------------------------------------------------------------
// Keyring helpers (per-(account, client_config))
// -----------------------------------------------------------------------------

func (s *Store) setOAuthAccessTokenForClientConfig(accountID, clientConfigID, token string) error {
	if token == "" {
		return nil
	}
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, accountID+":"+clientConfigID+":access_token", token)
		if err == nil {
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store extension access token in keyring")
	}
	// Mail configs share the encrypted_access_token column on accounts as a
	// fallback. Non-mail configs require the keyring (no per-(account, config)
	// fallback storage exists in Phase 1).
	if clientConfigID == "google-mail" || clientConfigID == "microsoft-mail" {
		return s.setOAuthAccessToken(accountID, token)
	}
	return fmt.Errorf("keyring unavailable and no fallback for client config %q", clientConfigID)
}

func (s *Store) getOAuthAccessTokenForClientConfig(accountID, clientConfigID string) (string, error) {
	// New per-(account, client_config) keyring entry — always preferred.
	if s.keyringEnabled {
		token, err := gokeyring.Get(serviceName, accountID+":"+clientConfigID+":access_token")
		if err == nil {
			return token, nil
		}
	}
	// Mail configs additionally honor the legacy single-config storage paths
	// (legacy keyring key OR encrypted DB column) for back-compat with tokens
	// written before migration v29. Non-mail configs require the new keyring
	// entry (no fallback storage exists for them in Phase 1).
	if clientConfigID == "google-mail" || clientConfigID == "microsoft-mail" {
		return s.getOAuthAccessToken(accountID)
	}
	return "", ErrCredentialNotFound
}

func (s *Store) setOAuthRefreshTokenForClientConfig(accountID, clientConfigID, token string) error {
	if token == "" {
		return nil
	}
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, accountID+":"+clientConfigID+":refresh_token", token)
		if err == nil {
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store extension refresh token in keyring")
	}
	if clientConfigID == "google-mail" || clientConfigID == "microsoft-mail" {
		return s.setOAuthRefreshToken(accountID, token)
	}
	return fmt.Errorf("keyring unavailable and no fallback for client config %q", clientConfigID)
}

func (s *Store) getOAuthRefreshTokenForClientConfig(accountID, clientConfigID string) (string, error) {
	if s.keyringEnabled {
		token, err := gokeyring.Get(serviceName, accountID+":"+clientConfigID+":refresh_token")
		if err == nil {
			return token, nil
		}
	}
	if clientConfigID == "google-mail" || clientConfigID == "microsoft-mail" {
		return s.getOAuthRefreshToken(accountID)
	}
	return "", ErrCredentialNotFound
}
