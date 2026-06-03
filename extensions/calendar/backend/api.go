package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// API is the extension-local logic the Bridge delegates to. NOT a
// coreapi.Calendar impl — the calendar extension doesn't expose one in
// Phase 1 (see docs/EXT_RULES.md R7: no speculative coreapi surfaces).
// All calendar CRUD lives behind the `Calendar_*` Wails bridge methods on
// `*CalendarBridge`.
//
// Holds:
//   - store:   the per-extension SQLite wrapper
//   - secrets: per-extension-scoped coreapi.Secrets handle (extensionID
//     pre-bound). All credential I/O goes through this surface; the API
//     never touches `internal/credentials` directly.
type API struct {
	store   *Store
	secrets coreapi.Secrets
}

// NewAPI constructs the API. Both deps are required.
func NewAPI(store *Store, secrets coreapi.Secrets) *API {
	return &API{store: store, secrets: secrets}
}

// AddCalDAVSource probes the user-entered server, persists the source +
// discovered calendars in a single transaction, and stores the password
// via coreapi.Secrets. Returns the new source ID.
//
// Atomicity:
//  1. Discovery is a transient probe — failure persists nothing.
//  2. Source + calendar inserts share one DB transaction.
//  3. After commit, secret write is attempted. On secret failure, the
//     source row (and its CASCADE'd calendars) is rolled back so we don't
//     leave a passwordless source.
func (a *API) AddCalDAVSource(name, serverURL, username, password string) (string, error) {
	if name == "" {
		return "", errors.New("calendar: name required")
	}
	if serverURL == "" {
		return "", errors.New("calendar: server URL required")
	}
	if username == "" {
		return "", errors.New("calendar: username required")
	}
	if password == "" {
		return "", errors.New("calendar: password required")
	}

	// 1. Probe.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	homePath, discovered, err := DiscoverCalendars(ctx, serverURL, username, password)
	if err != nil {
		return "", fmt.Errorf("discover calendars: %w", err)
	}
	if len(discovered) == 0 {
		return "", errors.New("calendar: no calendars found on server (server returned empty list — check the URL and that the account actually has calendars)")
	}

	sourceID := uuid.New().String()
	now := time.Now().Unix()

	// 2. Persist source + calendars atomically.
	err = a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.CreateSourceTx(tx, Source{
			ID:              sourceID,
			Type:            "caldav",
			Name:            name,
			URL:             homePath,
			Username:        username,
			SyncIntervalMin: 15,
			Enabled:         true,
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		for _, dc := range discovered {
			if err := a.store.CreateCalendarTx(tx, Calendar{
				ID:          uuid.New().String(),
				SourceID:    sourceID,
				URL:         dc.Path,
				DisplayName: dc.DisplayName,
				Description: dc.Description,
				Visible:     true,
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("persist source + calendars: %w", err)
	}

	// 3. Stash password. On failure, roll back the source row (CASCADE
	//    cleans up the calendars rows).
	if err := a.secrets.Set(sourceID, password); err != nil {
		_ = a.store.DeleteSource(sourceID)
		return "", fmt.Errorf("store password: %w", err)
	}

	return sourceID, nil
}

// ListSources returns all configured calendar sources.
func (a *API) ListSources() ([]Source, error) {
	return a.store.ListSources()
}

// ListCalendars returns calendars for one source.
func (a *API) ListCalendars(sourceID string) ([]Calendar, error) {
	if sourceID == "" {
		return nil, errors.New("calendar: source ID required")
	}
	return a.store.ListCalendars(sourceID)
}

// DeleteSource removes a source and all its calendars (via CASCADE) and
// clears its stored password. Best-effort on the secret delete — log + go,
// since the source row is what really matters.
func (a *API) DeleteSource(sourceID string) error {
	if sourceID == "" {
		return errors.New("calendar: source ID required")
	}
	// Best-effort secret cleanup BEFORE row deletion. If we delete the row
	// first and then crash, the secret is orphaned (but harmless — the
	// keyring entry just sits there, no row references it).
	_ = a.secrets.Delete(sourceID)
	return a.store.DeleteSource(sourceID)
}

// validSyncIntervals enumerates the values the frontend picker exposes.
// Reject other values to avoid hammering servers or wedging the scheduler.
var validSyncIntervals = map[int]struct{}{
	5: {}, 15: {}, 30: {}, 60: {}, 120: {}, 240: {}, 720: {},
}

// SetSyncInterval validates the minutes value, then writes it to the
// source row. The bridge restarts the per-source ticker after.
func (a *API) SetSyncInterval(sourceID string, minutes int) error {
	if sourceID == "" {
		return errors.New("calendar: source ID required")
	}
	if _, ok := validSyncIntervals[minutes]; !ok {
		return fmt.Errorf("calendar: invalid sync interval %d (allowed: 5/15/30/60/120/240/720)", minutes)
	}
	return a.store.UpdateSyncInterval(sourceID, minutes)
}
