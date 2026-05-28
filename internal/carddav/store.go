package carddav

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/rs/zerolog"
)

// Store handles CardDAV source and contact storage
type Store struct {
	db  *sql.DB
	log zerolog.Logger
}

// NewStore creates a new CardDAV store
func NewStore(db *sql.DB) *Store {
	return &Store{
		db:  db,
		log: logging.WithComponent("carddav-store"),
	}
}

// ============================================================================
// Source CRUD
// ============================================================================

// CreateSource creates a new contact source
func (s *Store) CreateSource(config *SourceConfig) (*Source, error) {
	id := uuid.New().String()
	now := time.Now()

	// Handle account_id (convert empty string to NULL)
	var accountID *string
	if config.AccountID != "" {
		accountID = &config.AccountID
	}

	query := `
		INSERT INTO contact_sources (id, name, type, url, username, account_id, enabled, sync_interval, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query,
		id, config.Name, config.Type, config.URL, config.Username, accountID,
		config.Enabled, config.SyncInterval, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create source: %w", err)
	}

	source := &Source{
		ID:           id,
		Name:         config.Name,
		Type:         config.Type,
		URL:          config.URL,
		Username:     config.Username,
		AccountID:    accountID,
		Enabled:      config.Enabled,
		SyncInterval: config.SyncInterval,
		CreatedAt:    now,
	}

	s.log.Info().Str("id", id).Str("name", config.Name).Msg("Contact source created")
	return source, nil
}

// GetSource returns a source by ID
func (s *Store) GetSource(id string) (*Source, error) {
	query := `
		SELECT id, name, type, url, username, account_id, enabled, sync_interval,
		       last_synced_at, last_error, last_error_at, created_at
		FROM contact_sources
		WHERE id = ?
	`

	var source Source
	var lastSyncedAt, lastErrorAt sql.NullTime
	var lastError, accountID sql.NullString

	err := s.db.QueryRow(query, id).Scan(
		&source.ID, &source.Name, &source.Type, &source.URL, &source.Username,
		&accountID, &source.Enabled, &source.SyncInterval,
		&lastSyncedAt, &lastError, &lastErrorAt, &source.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get source: %w", err)
	}

	if accountID.Valid {
		source.AccountID = &accountID.String
	}
	if lastSyncedAt.Valid {
		source.LastSyncedAt = &lastSyncedAt.Time
	}
	if lastError.Valid {
		source.LastError = lastError.String
	}
	if lastErrorAt.Valid {
		source.LastErrorAt = &lastErrorAt.Time
	}

	return &source, nil
}

// ListSources returns all contact sources
func (s *Store) ListSources() ([]*Source, error) {
	query := `
		SELECT id, name, type, url, username, account_id, enabled, sync_interval,
		       last_synced_at, last_error, last_error_at, created_at
		FROM contact_sources
		ORDER BY created_at ASC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}
	defer rows.Close()

	var sources []*Source
	for rows.Next() {
		var source Source
		var lastSyncedAt, lastErrorAt sql.NullTime
		var lastError, accountID sql.NullString

		err := rows.Scan(
			&source.ID, &source.Name, &source.Type, &source.URL, &source.Username,
			&accountID, &source.Enabled, &source.SyncInterval,
			&lastSyncedAt, &lastError, &lastErrorAt, &source.CreatedAt)
		if err != nil {
			s.log.Warn().Err(err).Msg("Failed to scan source row")
			continue
		}

		if accountID.Valid {
			source.AccountID = &accountID.String
		}
		if lastSyncedAt.Valid {
			source.LastSyncedAt = &lastSyncedAt.Time
		}
		if lastError.Valid {
			source.LastError = lastError.String
		}
		if lastErrorAt.Valid {
			source.LastErrorAt = &lastErrorAt.Time
		}

		sources = append(sources, &source)
	}

	return sources, nil
}

// UpdateSource updates a source's configuration
func (s *Store) UpdateSource(id string, config *SourceConfig) error {
	query := `
		UPDATE contact_sources
		SET name = ?, url = ?, username = ?, enabled = ?, sync_interval = ?
		WHERE id = ?
	`

	result, err := s.db.Exec(query,
		config.Name, config.URL, config.Username, config.Enabled, config.SyncInterval, id)
	if err != nil {
		return fmt.Errorf("failed to update source: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("source not found: %s", id)
	}

	s.log.Info().Str("id", id).Msg("Contact source updated")
	return nil
}

// DeleteSource deletes a source and all its addressbooks/contacts (via CASCADE)
func (s *Store) DeleteSource(id string) error {
	result, err := s.db.Exec("DELETE FROM contact_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete source: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("source not found: %s", id)
	}

	s.log.Info().Str("id", id).Msg("Contact source deleted")
	return nil
}

// UpdateSourceSyncStatus updates the sync status after a sync attempt
func (s *Store) UpdateSourceSyncStatus(id string, syncError string) error {
	now := time.Now()

	var query string
	var args []interface{}

	if syncError == "" {
		// Success: update last_synced_at, clear error
		query = `
			UPDATE contact_sources
			SET last_synced_at = ?, last_error = NULL, last_error_at = NULL
			WHERE id = ?
		`
		args = []interface{}{now, id}
	} else {
		// Error: update error fields
		query = `
			UPDATE contact_sources
			SET last_error = ?, last_error_at = ?
			WHERE id = ?
		`
		args = []interface{}{syncError, now, id}
	}

	_, err := s.db.Exec(query, args...)
	return err
}

// ClearSourceError clears the error for a source
func (s *Store) ClearSourceError(id string) error {
	query := `
		UPDATE contact_sources
		SET last_error = NULL, last_error_at = NULL
		WHERE id = ?
	`
	_, err := s.db.Exec(query, id)
	return err
}

// GetSourcesWithErrors returns all sources that have errors
func (s *Store) GetSourcesWithErrors() ([]*SourceError, error) {
	query := `
		SELECT id, name, last_error, last_error_at
		FROM contact_sources
		WHERE last_error IS NOT NULL AND last_error != ''
		ORDER BY last_error_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get sources with errors: %w", err)
	}
	defer rows.Close()

	var errors []*SourceError
	for rows.Next() {
		var se SourceError
		var errorAt sql.NullTime

		err := rows.Scan(&se.SourceID, &se.SourceName, &se.Error, &errorAt)
		if err != nil {
			continue
		}

		if errorAt.Valid {
			se.ErrorAt = errorAt.Time
		}

		errors = append(errors, &se)
	}

	return errors, nil
}

// ============================================================================
// Addressbook CRUD
// ============================================================================

// CreateAddressbook creates a new addressbook for a source
func (s *Store) CreateAddressbook(sourceID, path, name string, enabled bool) (*Addressbook, error) {
	id := uuid.New().String()

	query := `
		INSERT INTO contact_source_addressbooks (id, source_id, path, name, enabled)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query, id, sourceID, path, name, enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to create addressbook: %w", err)
	}

	return &Addressbook{
		ID:       id,
		SourceID: sourceID,
		Path:     path,
		Name:     name,
		Enabled:  enabled,
	}, nil
}

// GetAddressbook returns an addressbook by ID
func (s *Store) GetAddressbook(id string) (*Addressbook, error) {
	query := `
		SELECT id, source_id, path, name, enabled, sync_token, last_synced_at
		FROM contact_source_addressbooks
		WHERE id = ?
	`

	var ab Addressbook
	var syncToken sql.NullString
	var lastSyncedAt sql.NullTime

	err := s.db.QueryRow(query, id).Scan(
		&ab.ID, &ab.SourceID, &ab.Path, &ab.Name, &ab.Enabled,
		&syncToken, &lastSyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get addressbook: %w", err)
	}

	if syncToken.Valid {
		ab.SyncToken = syncToken.String
	}
	if lastSyncedAt.Valid {
		ab.LastSyncedAt = &lastSyncedAt.Time
	}

	return &ab, nil
}

// ListAddressbooks returns all addressbooks for a source
func (s *Store) ListAddressbooks(sourceID string) ([]*Addressbook, error) {
	query := `
		SELECT id, source_id, path, name, enabled, sync_token, last_synced_at
		FROM contact_source_addressbooks
		WHERE source_id = ?
		ORDER BY name ASC
	`

	rows, err := s.db.Query(query, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list addressbooks: %w", err)
	}
	defer rows.Close()

	var addressbooks []*Addressbook
	for rows.Next() {
		var ab Addressbook
		var syncToken sql.NullString
		var lastSyncedAt sql.NullTime

		err := rows.Scan(
			&ab.ID, &ab.SourceID, &ab.Path, &ab.Name, &ab.Enabled,
			&syncToken, &lastSyncedAt)
		if err != nil {
			continue
		}

		if syncToken.Valid {
			ab.SyncToken = syncToken.String
		}
		if lastSyncedAt.Valid {
			ab.LastSyncedAt = &lastSyncedAt.Time
		}

		addressbooks = append(addressbooks, &ab)
	}

	return addressbooks, nil
}

// ListEnabledAddressbooks returns all enabled addressbooks for a source
func (s *Store) ListEnabledAddressbooks(sourceID string) ([]*Addressbook, error) {
	query := `
		SELECT id, source_id, path, name, enabled, sync_token, last_synced_at
		FROM contact_source_addressbooks
		WHERE source_id = ? AND enabled = 1
		ORDER BY name ASC
	`

	rows, err := s.db.Query(query, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled addressbooks: %w", err)
	}
	defer rows.Close()

	var addressbooks []*Addressbook
	for rows.Next() {
		var ab Addressbook
		var syncToken sql.NullString
		var lastSyncedAt sql.NullTime

		err := rows.Scan(
			&ab.ID, &ab.SourceID, &ab.Path, &ab.Name, &ab.Enabled,
			&syncToken, &lastSyncedAt)
		if err != nil {
			continue
		}

		if syncToken.Valid {
			ab.SyncToken = syncToken.String
		}
		if lastSyncedAt.Valid {
			ab.LastSyncedAt = &lastSyncedAt.Time
		}

		addressbooks = append(addressbooks, &ab)
	}

	return addressbooks, nil
}

// SetAddressbookEnabled enables or disables an addressbook
func (s *Store) SetAddressbookEnabled(id string, enabled bool) error {
	query := `UPDATE contact_source_addressbooks SET enabled = ? WHERE id = ?`
	_, err := s.db.Exec(query, enabled, id)
	return err
}

// UpdateAddressbookSyncToken updates the sync token after a sync
func (s *Store) UpdateAddressbookSyncToken(id, syncToken string) error {
	now := time.Now()
	query := `
		UPDATE contact_source_addressbooks
		SET sync_token = ?, last_synced_at = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query, syncToken, now, id)
	return err
}

// DeleteAddressbooksForSource deletes all addressbooks for a source
func (s *Store) DeleteAddressbooksForSource(sourceID string) error {
	_, err := s.db.Exec("DELETE FROM contact_source_addressbooks WHERE source_id = ?", sourceID)
	return err
}

// ============================================================================
// Contact CRUD
// ============================================================================

// UpsertContact creates or updates a contact
func (s *Store) UpsertContact(contact *Contact) error {
	if contact.ID == "" {
		contact.ID = uuid.New().String()
	}
	contact.SyncedAt = time.Now()

	query := `
		INSERT INTO carddav_contacts (id, addressbook_id, email, display_name, href, etag, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email = excluded.email,
			display_name = excluded.display_name,
			href = excluded.href,
			etag = excluded.etag,
			synced_at = excluded.synced_at
	`

	_, err := s.db.Exec(query,
		contact.ID, contact.AddressbookID, contact.Email, contact.DisplayName,
		contact.Href, contact.ETag, contact.SyncedAt)
	return err
}

// UpsertContactsBatch creates or updates multiple contacts in a single transaction.
// This is much faster than calling UpsertContact individually for each contact.
func (s *Store) UpsertContactsBatch(contacts []*Contact) error {
	if len(contacts) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statement once, execute many times
	stmt, err := tx.Prepare(`
		INSERT INTO carddav_contacts (id, addressbook_id, email, display_name, href, etag, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email = excluded.email,
			display_name = excluded.display_name,
			href = excluded.href,
			etag = excluded.etag,
			synced_at = excluded.synced_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	inserted := 0

	for _, c := range contacts {
		if c.ID == "" {
			c.ID = uuid.New().String()
		}
		c.SyncedAt = now

		_, err := stmt.Exec(c.ID, c.AddressbookID, c.Email, c.DisplayName, c.Href, c.ETag, c.SyncedAt)
		if err != nil {
			s.log.Warn().Err(err).Str("email", c.Email).Msg("Failed to upsert contact in batch")
			continue
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.log.Debug().Int("inserted", inserted).Int("total", len(contacts)).Msg("Batch upsert complete")
	return nil
}

// DeleteContactsForAddressbook deletes all contacts for an addressbook
func (s *Store) DeleteContactsForAddressbook(addressbookID string) error {
	_, err := s.db.Exec("DELETE FROM carddav_contacts WHERE addressbook_id = ?", addressbookID)
	return err
}

// DeleteContactByHref deletes a contact by its href
func (s *Store) DeleteContactByHref(addressbookID, href string) error {
	_, err := s.db.Exec(
		"DELETE FROM carddav_contacts WHERE addressbook_id = ? AND href = ?",
		addressbookID, href)
	return err
}

// DeleteContactsByHrefs deletes multiple contacts by their hrefs in a single transaction
func (s *Store) DeleteContactsByHrefs(addressbookID string, hrefs []string) error {
	if len(hrefs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM carddav_contacts WHERE addressbook_id = ? AND href = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, href := range hrefs {
		if _, err := stmt.Exec(addressbookID, href); err != nil {
			return fmt.Errorf("failed to delete contact with href %s: %w", href, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.log.Debug().Int("count", len(hrefs)).Msg("Batch delete complete")
	return nil
}

// SearchContacts searches CardDAV contacts by query
func (s *Store) SearchContacts(query string, limit int) ([]*Contact, error) {
	if limit <= 0 {
		limit = 10
	}

	pattern := "%" + strings.ToLower(query) + "%"

	sqlQuery := `
		SELECT c.id, c.addressbook_id, c.email, c.display_name, c.href, c.etag, c.synced_at
		FROM carddav_contacts c
		JOIN contact_source_addressbooks ab ON c.addressbook_id = ab.id
		JOIN contact_sources s ON ab.source_id = s.id
		WHERE s.enabled = 1 AND ab.enabled = 1
		  AND (LOWER(c.email) LIKE ? OR LOWER(c.display_name) LIKE ?)
		ORDER BY c.display_name ASC
		LIMIT ?
	`

	rows, err := s.db.Query(sqlQuery, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search contacts: %w", err)
	}
	defer rows.Close()

	var contacts []*Contact
	for rows.Next() {
		var c Contact
		err := rows.Scan(
			&c.ID, &c.AddressbookID, &c.Email, &c.DisplayName,
			&c.Href, &c.ETag, &c.SyncedAt)
		if err != nil {
			continue
		}
		contacts = append(contacts, &c)
	}

	return contacts, nil
}

// ListContactsPaged returns contacts for a single source in display-name
// order, with offset/limit paging and optional case-insensitive query filter
// on email / display_name (pass "" for no filter). Only contacts from enabled
// addressbooks of an enabled source are returned (matches SearchContacts
// visibility). Used by the Contacts extension's browse UI.
func (s *Store) ListContactsPaged(sourceID, query string, offset, limit int) ([]*Contact, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	sqlQuery := `
		SELECT c.id, c.addressbook_id, c.email, c.display_name, c.href, c.etag, c.synced_at
		FROM carddav_contacts c
		JOIN contact_source_addressbooks ab ON c.addressbook_id = ab.id
		JOIN contact_sources s ON ab.source_id = s.id
		WHERE s.id = ? AND s.enabled = 1 AND ab.enabled = 1
		  AND (? = '' OR LOWER(c.email) LIKE ? OR LOWER(c.display_name) LIKE ?)
		ORDER BY c.display_name ASC, c.email ASC
		LIMIT ? OFFSET ?
	`

	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.Query(sqlQuery, sourceID, query, pattern, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts for source %s: %w", sourceID, err)
	}
	defer rows.Close()

	var contacts []*Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.AddressbookID, &c.Email, &c.DisplayName, &c.Href, &c.ETag, &c.SyncedAt); err != nil {
			s.log.Warn().Err(err).Msg("Failed to scan contact row")
			continue
		}
		contacts = append(contacts, &c)
	}

	return contacts, rows.Err()
}

// GetContactByID returns a contact by its primary-key id (carddav UUID).
// Returns (nil, nil) when not found.
func (s *Store) GetContactByID(id string) (*Contact, error) {
	query := `
		SELECT id, addressbook_id, email, display_name, href, etag, synced_at
		FROM carddav_contacts
		WHERE id = ?
	`

	var c Contact
	err := s.db.QueryRow(query, id).Scan(
		&c.ID, &c.AddressbookID, &c.Email, &c.DisplayName,
		&c.Href, &c.ETag, &c.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get contact by id: %w", err)
	}
	return &c, nil
}

// GetContactByHref returns a contact by its href (for update checks)
func (s *Store) GetContactByHref(addressbookID, href string) (*Contact, error) {
	query := `
		SELECT id, addressbook_id, email, display_name, href, etag, synced_at
		FROM carddav_contacts
		WHERE addressbook_id = ? AND href = ?
	`

	var c Contact
	err := s.db.QueryRow(query, addressbookID, href).Scan(
		&c.ID, &c.AddressbookID, &c.Email, &c.DisplayName,
		&c.Href, &c.ETag, &c.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// CountContacts returns the total number of CardDAV contacts
func (s *Store) CountContacts() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM carddav_contacts").Scan(&count)
	return count, err
}

// CountContactsForSource returns the number of contacts for a source
func (s *Store) CountContactsForSource(sourceID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM carddav_contacts c
		JOIN contact_source_addressbooks ab ON c.addressbook_id = ab.id
		WHERE ab.source_id = ?
	`
	var count int
	err := s.db.QueryRow(query, sourceID).Scan(&count)
	return count, err
}

// GetSourceByAccountID returns a contact source linked to an email account
func (s *Store) GetSourceByAccountID(accountID string) (*Source, error) {
	query := `
		SELECT id, name, type, url, username, account_id, enabled, sync_interval,
		       last_synced_at, last_error, last_error_at, created_at
		FROM contact_sources
		WHERE account_id = ?
	`

	var source Source
	var lastSyncedAt, lastErrorAt sql.NullTime
	var lastError, accID sql.NullString

	err := s.db.QueryRow(query, accountID).Scan(
		&source.ID, &source.Name, &source.Type, &source.URL, &source.Username,
		&accID, &source.Enabled, &source.SyncInterval,
		&lastSyncedAt, &lastError, &lastErrorAt, &source.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get source by account ID: %w", err)
	}

	if accID.Valid {
		source.AccountID = &accID.String
	}
	if lastSyncedAt.Valid {
		source.LastSyncedAt = &lastSyncedAt.Time
	}
	if lastError.Valid {
		source.LastError = lastError.String
	}
	if lastErrorAt.Valid {
		source.LastErrorAt = &lastErrorAt.Time
	}

	return &source, nil
}
