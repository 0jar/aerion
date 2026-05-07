// Package credentials provides secure credential storage with fallback support
package credentials

import (
	"database/sql"
	"fmt"

	"github.com/hkdb/aerion/internal/crypto"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/rs/zerolog"
	gokeyring "github.com/zalando/go-keyring"
)

const serviceName = "aerion"

// Store provides credential storage with OS keyring and encrypted DB fallback
type Store struct {
	db             *sql.DB
	encryptor      *crypto.Encryptor
	keyringEnabled bool
	log            zerolog.Logger
}

// NewStore creates a new credential store
// It tries to use the OS keyring, falling back to encrypted database storage
func NewStore(db *sql.DB, dataDir string) (*Store, error) {
	log := logging.WithComponent("credentials")

	// Create encryptor for fallback storage
	encryptor, err := crypto.NewEncryptor(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	// Test if keyring is available
	keyringEnabled := testKeyring()
	if keyringEnabled {
		log.Info().Msg("OS keyring available, using as primary credential storage")
	} else {
		log.Warn().Msg("OS keyring not available, using encrypted database storage")
	}

	return &Store{
		db:             db,
		encryptor:      encryptor,
		keyringEnabled: keyringEnabled,
		log:            log,
	}, nil
}

// testKeyring checks if the OS keyring is available and functional
func testKeyring() bool {
	testKey := "aerion-test-keyring-check"
	testValue := "test"

	// Try to set a test value
	err := gokeyring.Set(serviceName, testKey, testValue)
	if err != nil {
		return false
	}

	// Clean up test value
	_ = gokeyring.Delete(serviceName, testKey)

	return true
}

// SetPassword stores a password for an account
func (s *Store) SetPassword(accountID, password string) error {
	if password == "" {
		return nil
	}

	// Try OS keyring first if available
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, accountID, password)
		if err == nil {
			s.log.Debug().Str("account_id", accountID).Msg("Password stored in OS keyring")
			// Clear any fallback storage
			s.clearDBPassword(accountID)
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store in OS keyring, using fallback")
	}

	// Fallback to encrypted database storage
	encrypted, err := s.encryptor.Encrypt(password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	_, err = s.db.Exec(
		"UPDATE accounts SET encrypted_password = ? WHERE id = ?",
		encrypted, accountID,
	)
	if err != nil {
		return fmt.Errorf("failed to store encrypted password: %w", err)
	}

	s.log.Debug().Str("account_id", accountID).Msg("Password stored in encrypted database")
	return nil
}

// GetPassword retrieves a password for an account
func (s *Store) GetPassword(accountID string) (string, error) {
	// Try OS keyring first if available
	if s.keyringEnabled {
		password, err := gokeyring.Get(serviceName, accountID)
		if err == nil {
			return password, nil
		}
		if err != gokeyring.ErrNotFound {
			s.log.Warn().Err(err).Msg("Error reading from OS keyring, trying fallback")
		}
	}

	// Try fallback encrypted database storage
	var encrypted sql.NullString
	err := s.db.QueryRow(
		"SELECT encrypted_password FROM accounts WHERE id = ?",
		accountID,
	).Scan(&encrypted)

	if err == sql.ErrNoRows {
		return "", ErrCredentialNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to query password: %w", err)
	}

	if !encrypted.Valid || encrypted.String == "" {
		return "", ErrCredentialNotFound
	}

	// Decrypt
	password, err := s.encryptor.Decrypt(encrypted.String)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt password: %w", err)
	}

	return password, nil
}

// DeletePassword removes a password for an account
func (s *Store) DeletePassword(accountID string) error {
	// Delete from OS keyring
	if s.keyringEnabled {
		_ = gokeyring.Delete(serviceName, accountID)
	}

	// Delete from database
	s.clearDBPassword(accountID)

	return nil
}

// clearDBPassword clears the encrypted password from the database
func (s *Store) clearDBPassword(accountID string) {
	_, _ = s.db.Exec("UPDATE accounts SET encrypted_password = NULL WHERE id = ?", accountID)
}

// DeleteAllCredentials removes all credentials for an account
func (s *Store) DeleteAllCredentials(accountID string) error {
	_ = s.DeletePassword(accountID)
	_ = s.DeleteOAuthTokens(accountID)
	return nil
}

// IsKeyringEnabled returns whether the OS keyring is being used
func (s *Store) IsKeyringEnabled() bool {
	return s.keyringEnabled
}

// SetSMIMEPrivateKey stores an S/MIME private key for a certificate
func (s *Store) SetSMIMEPrivateKey(certID string, privateKeyPEM []byte) error {
	if len(privateKeyPEM) == 0 {
		return nil
	}

	keyringKey := "smime:" + certID + ":private_key"

	// Try OS keyring first if available
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, keyringKey, string(privateKeyPEM))
		if err == nil {
			s.log.Debug().Str("cert_id", certID).Msg("S/MIME private key stored in OS keyring")
			s.clearSMIMEDBPrivateKey(certID)
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store S/MIME key in OS keyring, using fallback")
	}

	// Fallback to encrypted database storage
	encrypted, err := s.encryptor.Encrypt(string(privateKeyPEM))
	if err != nil {
		return fmt.Errorf("failed to encrypt S/MIME private key: %w", err)
	}

	_, err = s.db.Exec(
		"UPDATE smime_certificates SET encrypted_private_key = ? WHERE id = ?",
		encrypted, certID,
	)
	if err != nil {
		return fmt.Errorf("failed to store encrypted S/MIME private key: %w", err)
	}

	s.log.Debug().Str("cert_id", certID).Msg("S/MIME private key stored in encrypted database")
	return nil
}

// GetSMIMEPrivateKey retrieves an S/MIME private key for a certificate
func (s *Store) GetSMIMEPrivateKey(certID string) ([]byte, error) {
	keyringKey := "smime:" + certID + ":private_key"

	// Try OS keyring first if available
	if s.keyringEnabled {
		key, err := gokeyring.Get(serviceName, keyringKey)
		if err == nil {
			return []byte(key), nil
		}
		if err != gokeyring.ErrNotFound {
			s.log.Warn().Err(err).Msg("Error reading S/MIME key from OS keyring, trying fallback")
		}
	}

	// Try fallback encrypted database storage
	var encrypted sql.NullString
	err := s.db.QueryRow(
		"SELECT encrypted_private_key FROM smime_certificates WHERE id = ?",
		certID,
	).Scan(&encrypted)

	if err == sql.ErrNoRows {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query S/MIME private key: %w", err)
	}

	if !encrypted.Valid || encrypted.String == "" {
		return nil, ErrCredentialNotFound
	}

	key, err := s.encryptor.Decrypt(encrypted.String)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt S/MIME private key: %w", err)
	}

	return []byte(key), nil
}

// DeleteSMIMEPrivateKey removes an S/MIME private key for a certificate
func (s *Store) DeleteSMIMEPrivateKey(certID string) error {
	keyringKey := "smime:" + certID + ":private_key"

	if s.keyringEnabled {
		_ = gokeyring.Delete(serviceName, keyringKey)
	}

	s.clearSMIMEDBPrivateKey(certID)
	return nil
}

// clearSMIMEDBPrivateKey clears the encrypted private key from the database
func (s *Store) clearSMIMEDBPrivateKey(certID string) {
	_, _ = s.db.Exec("UPDATE smime_certificates SET encrypted_private_key = NULL WHERE id = ?", certID)
}

// SetPGPPrivateKey stores a PGP private key for a keypair
func (s *Store) SetPGPPrivateKey(keyID string, armoredKey []byte) error {
	if len(armoredKey) == 0 {
		return nil
	}

	keyringKey := "pgp:" + keyID + ":private_key"

	// Try OS keyring first if available
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, keyringKey, string(armoredKey))
		if err == nil {
			s.log.Debug().Str("key_id", keyID).Msg("PGP private key stored in OS keyring")
			s.clearPGPDBPrivateKey(keyID)
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store PGP key in OS keyring, using fallback")
	}

	// Fallback to encrypted database storage
	encrypted, err := s.encryptor.Encrypt(string(armoredKey))
	if err != nil {
		return fmt.Errorf("failed to encrypt PGP private key: %w", err)
	}

	_, err = s.db.Exec(
		"UPDATE pgp_keys SET encrypted_private_key = ? WHERE id = ?",
		encrypted, keyID,
	)
	if err != nil {
		return fmt.Errorf("failed to store encrypted PGP private key: %w", err)
	}

	s.log.Debug().Str("key_id", keyID).Msg("PGP private key stored in encrypted database")
	return nil
}

// GetPGPPrivateKey retrieves a PGP private key for a keypair
func (s *Store) GetPGPPrivateKey(keyID string) ([]byte, error) {
	keyringKey := "pgp:" + keyID + ":private_key"

	// Try OS keyring first if available
	if s.keyringEnabled {
		key, err := gokeyring.Get(serviceName, keyringKey)
		if err == nil {
			return []byte(key), nil
		}
		if err != gokeyring.ErrNotFound {
			s.log.Warn().Err(err).Msg("Error reading PGP key from OS keyring, trying fallback")
		}
	}

	// Try fallback encrypted database storage
	var encrypted sql.NullString
	err := s.db.QueryRow(
		"SELECT encrypted_private_key FROM pgp_keys WHERE id = ?",
		keyID,
	).Scan(&encrypted)

	if err == sql.ErrNoRows {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query PGP private key: %w", err)
	}

	if !encrypted.Valid || encrypted.String == "" {
		return nil, ErrCredentialNotFound
	}

	key, err := s.encryptor.Decrypt(encrypted.String)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt PGP private key: %w", err)
	}

	return []byte(key), nil
}

// DeletePGPPrivateKey removes a PGP private key for a keypair
func (s *Store) DeletePGPPrivateKey(keyID string) error {
	keyringKey := "pgp:" + keyID + ":private_key"

	if s.keyringEnabled {
		_ = gokeyring.Delete(serviceName, keyringKey)
	}

	s.clearPGPDBPrivateKey(keyID)
	return nil
}

// clearPGPDBPrivateKey clears the encrypted private key from the database
func (s *Store) clearPGPDBPrivateKey(keyID string) {
	_, _ = s.db.Exec("UPDATE pgp_keys SET encrypted_private_key = NULL WHERE id = ?", keyID)
}

// SetCardDAVPassword stores a password for a CardDAV contact source
func (s *Store) SetCardDAVPassword(sourceID, password string) error {
	if password == "" {
		return nil
	}

	// Try OS keyring first if available
	if s.keyringEnabled {
		err := gokeyring.Set(serviceName, "carddav:"+sourceID, password)
		if err == nil {
			s.log.Debug().Str("source_id", sourceID).Msg("CardDAV password stored in OS keyring")
			// Clear any fallback storage
			s.clearCardDAVDBPassword(sourceID)
			return nil
		}
		s.log.Warn().Err(err).Msg("Failed to store CardDAV password in OS keyring, using fallback")
	}

	// Fallback to encrypted database storage
	encrypted, err := s.encryptor.Encrypt(password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	_, err = s.db.Exec(
		"UPDATE contact_sources SET encrypted_password = ? WHERE id = ?",
		encrypted, sourceID,
	)
	if err != nil {
		return fmt.Errorf("failed to store encrypted password: %w", err)
	}

	s.log.Debug().Str("source_id", sourceID).Msg("CardDAV password stored in encrypted database")
	return nil
}

// GetCardDAVPassword retrieves a password for a CardDAV contact source
func (s *Store) GetCardDAVPassword(sourceID string) (string, error) {
	// Try OS keyring first if available
	if s.keyringEnabled {
		password, err := gokeyring.Get(serviceName, "carddav:"+sourceID)
		if err == nil {
			return password, nil
		}
		if err != gokeyring.ErrNotFound {
			s.log.Warn().Err(err).Msg("Error reading CardDAV password from OS keyring, trying fallback")
		}
	}

	// Try fallback encrypted database storage
	var encrypted sql.NullString
	err := s.db.QueryRow(
		"SELECT encrypted_password FROM contact_sources WHERE id = ?",
		sourceID,
	).Scan(&encrypted)

	if err == sql.ErrNoRows {
		return "", ErrCredentialNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to query password: %w", err)
	}

	if !encrypted.Valid || encrypted.String == "" {
		return "", ErrCredentialNotFound
	}

	// Decrypt
	password, err := s.encryptor.Decrypt(encrypted.String)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt password: %w", err)
	}

	return password, nil
}

// DeleteCardDAVPassword removes a password for a CardDAV contact source
func (s *Store) DeleteCardDAVPassword(sourceID string) error {
	// Delete from OS keyring
	if s.keyringEnabled {
		_ = gokeyring.Delete(serviceName, "carddav:"+sourceID)
	}

	// Delete from database
	s.clearCardDAVDBPassword(sourceID)

	return nil
}

// clearCardDAVDBPassword clears the encrypted password from the contact_sources table
func (s *Store) clearCardDAVDBPassword(sourceID string) {
	_, _ = s.db.Exec("UPDATE contact_sources SET encrypted_password = NULL WHERE id = ?", sourceID)
}
