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
//   - auth:    coreapi.Auth handle for OAuth-vended *http.Client. Used by
//     the googleProvider + microsoftProvider write paths through
//     ProviderDeps. Nil disables Google/Microsoft writes (caldav + local
//     keep working).
type API struct {
	store   *Store
	secrets coreapi.Secrets
	auth    coreapi.Auth
	queue   *PendingQueue
}

// NewAPI constructs the API. store + secrets are required; auth and queue
// may be nil (CalDAV + local stay functional without auth; the soft-commit
// path is skipped without a queue).
func NewAPI(store *Store, secrets coreapi.Secrets, auth coreapi.Auth, queue *PendingQueue) *API {
	return &API{store: store, secrets: secrets, auth: auth, queue: queue}
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

	// 2a. Probe RFC 6638 scheduling support so the composer can gate the
	// "Send invitations" toggle for this source. Best-effort: any error
	// defaults to "server" — the toggle stays available, and the worst
	// case is a no-op delivery on a non-6638 server (same as pre-v0.3.0).
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	itipMode := probeCalDAVScheduling(probeCtx, serverURL, username, password)
	probeCancel()

	// 2b. Persist source + calendars atomically.
	err = a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.CreateSourceTx(tx, Source{
			ID:              sourceID,
			Type:            SourceTypeCalDAV,
			Name:            name,
			URL:             homePath,
			Username:        username,
			SyncIntervalMin: 15,
			Enabled:         true,
			Writable:        true, // trust-on-first-write per RFC 4791
			CreatedAt:       now,
			ITIPMode:        itipMode,
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
				Writable:    dc.Writable,
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

// AddLocalSource creates a `calendar_sources` row with type='local'. Unlike
// AddCalDAVSource, there's no network probe, no password, and no
// auto-discovered calendars — local sources start empty and the user adds
// calendars under them via AddLocalCalendar.
//
// Idempotent on (name, type='local'): if a local source with the same
// display name already exists, returns its ID. Lets the frontend safely
// call AddLocalSource("Local") repeatedly without creating duplicates.
func (a *API) AddLocalSource(name string) (string, error) {
	if name == "" {
		return "", errors.New("calendar: name required")
	}

	// Look up by name + type. If found, return existing ID.
	existing, err := a.store.ListSources()
	if err != nil {
		return "", fmt.Errorf("list sources: %w", err)
	}
	for _, s := range existing {
		if s.Type == SourceTypeLocal && s.Name == name {
			return s.ID, nil
		}
	}

	sourceID := uuid.New().String()
	now := time.Now().Unix()
	err = a.store.WithTx(func(tx *sql.Tx) error {
		return a.store.CreateSourceTx(tx, Source{
			ID:              sourceID,
			Type:            SourceTypeLocal,
			Name:            name,
			URL:             "",
			Username:        "",
			SyncIntervalMin: 0, // unused for local
			Enabled:         true,
			Writable:        true, // local sources support full CRUD
			CreatedAt:       now,
		})
	})
	if err != nil {
		return "", fmt.Errorf("persist local source: %w", err)
	}
	return sourceID, nil
}

// DeleteCalendar removes a calendar. Only local-source calendars are
// deletable from Aerion — CalDAV calendars belong to the remote server
// and would be re-synced. CASCADE removes all events + overrides + alarms
// belonging to the calendar.
func (a *API) DeleteCalendar(calendarID string) error {
	if calendarID == "" {
		return errors.New("calendar: calendar ID required")
	}
	cal, err := a.store.GetCalendar(calendarID)
	if err != nil {
		return fmt.Errorf("look up calendar: %w", err)
	}
	if cal == nil {
		return nil // idempotent
	}
	src, err := a.store.GetSource(cal.SourceID)
	if err != nil {
		return fmt.Errorf("look up source: %w", err)
	}
	if src == nil {
		return errors.New("calendar: source not found for calendar")
	}
	if src.Type != SourceTypeLocal {
		return fmt.Errorf("calendar: only local calendars can be deleted from Aerion (source type=%q)", src.Type)
	}
	return a.store.DeleteCalendar(calendarID)
}

// AddLocalCalendar inserts a `calendars` row under a local source. Validates
// that the source exists and is type='local'. Color is optional — empty
// string falls back to the frontend's HSL hash via colorOfHex/colorOf.
func (a *API) AddLocalCalendar(sourceID, displayName, color string) (string, error) {
	if sourceID == "" {
		return "", errors.New("calendar: source ID required")
	}
	if displayName == "" {
		return "", errors.New("calendar: display name required")
	}
	src, err := a.store.GetSource(sourceID)
	if err != nil {
		return "", fmt.Errorf("look up source: %w", err)
	}
	if src == nil {
		return "", errors.New("calendar: source not found")
	}
	if src.Type != SourceTypeLocal {
		return "", fmt.Errorf("calendar: source %q is not a local source (type=%q)", sourceID, src.Type)
	}

	calendarID := uuid.New().String()
	now := time.Now().Unix()
	err = a.store.WithTx(func(tx *sql.Tx) error {
		return a.store.CreateCalendarTx(tx, Calendar{
			ID:       calendarID,
			SourceID: sourceID,
			// Per-calendar synthetic URL so the (source_id, url) UNIQUE
			// constraint stays satisfied across multiple local calendars
			// under the same source. CalDAV calendars use the server path;
			// local calendars use "local:<uuid>" — opaque to the user and
			// guaranteed unique by the UUID.
			URL:         "local:" + calendarID,
			DisplayName: displayName,
			Description: "",
			Color:       color,
			Visible:     true,
			Writable:    true, // local calendars are always writable — we own the storage
			CreatedAt:   now,
		})
	})
	if err != nil {
		return "", fmt.Errorf("persist local calendar: %w", err)
	}
	return calendarID, nil
}

// --- Google Calendar source setup --------------------------------------------
//
// AddGoogleSource creates a calendar_sources row tied to an existing Aerion
// mail account (AccountID identifies the Gmail account that holds the OAuth
// grant). The frontend picker calls ListGoogleCalendarsForAccount first to
// let the user pick which calendars to subscribe; AddGoogleSource then
// persists that selection.

// GoogleCalendarChoice is one calendar surfaced to the picker. JSON tags
// drive the TS binding shape.
type GoogleCalendarChoice struct {
	ID         string `json:"id"`         // Google's calendar id (e.g. "primary" or "{hash}@group.calendar.google.com")
	Summary    string `json:"summary"`    // user-visible name
	Primary    bool   `json:"primary"`    // user's main calendar
	AccessRole string `json:"accessRole"` // "owner" | "writer" | "reader" | "freeBusyReader"
	Writable   bool   `json:"writable"`   // derived: true if accessRole is owner or writer
}

// GoogleCalendarSelection is one entry the frontend passes back to
// AddGoogleSource — id + display name from the picker. Writable mirrors
// the picker's GoogleCalendarChoice.Writable (derived from AccessRole)
// so we persist per-calendar permissions at add time without re-probing
// Google's API.
type GoogleCalendarSelection struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Color       string `json:"color,omitempty"`
	Writable    bool   `json:"writable"`
}

// ListGoogleCalendarsForAccount drives Google's /users/me/calendarList using
// the account's OAuth grant. Returns the user's calendars filtered for ones
// they can write to (Chunk 3 ships writer/owner only; read-only and
// freeBusyReader are surfaced but the picker filters them client-side).
func (a *API) ListGoogleCalendarsForAccount(ctx context.Context, accountID string) ([]GoogleCalendarChoice, error) {
	if accountID == "" {
		return nil, errors.New("calendar: account ID required")
	}
	if a.auth == nil {
		return nil, errors.New("calendar: OAuth handle not configured")
	}

	// Transient Source — never persisted. Lets googleProvider.httpClient
	// build the right OAuth request without us needing a parallel "list
	// for unsaved source" code path.
	transient := Source{Type: SourceTypeGoogle, AccountID: accountID}
	p := googleProvider{store: a.store, auth: a.auth}

	entries, err := p.ListGoogleCalendars(ctx, transient)
	if err != nil {
		return nil, err
	}

	out := make([]GoogleCalendarChoice, 0, len(entries))
	for _, e := range entries {
		writable := e.AccessRole == "owner" || e.AccessRole == "writer"
		out = append(out, GoogleCalendarChoice{
			ID:         e.ID,
			Summary:    e.Summary,
			Primary:    e.Primary,
			AccessRole: e.AccessRole,
			Writable:   writable,
		})
	}
	return out, nil
}

// --- Microsoft Graph Calendar source setup -----------------------------------
//
// Mirrors the Google flow. See ListGoogleCalendarsForAccount / AddGoogleSource
// for the equivalent comments.

// MicrosoftCalendarChoice is one calendar surfaced to the Microsoft picker.
type MicrosoftCalendarChoice struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	IsDefaultCalendar bool   `json:"isDefaultCalendar"`
	Writable          bool   `json:"writable"` // derived from canEdit
}

// MicrosoftCalendarSelection is one entry the frontend passes back to
// AddMicrosoftSource — id + display name from the picker. Writable mirrors
// the picker's MicrosoftCalendarChoice.Writable (Graph canEdit) so we
// persist per-calendar permissions at add time.
type MicrosoftCalendarSelection struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Color       string `json:"color,omitempty"`
	Writable    bool   `json:"writable"`
}

// ListMicrosoftCalendarsForAccount drives Graph's /me/calendars using the
// account's OAuth grant. Returns the user's calendars with writability
// derived from canEdit.
func (a *API) ListMicrosoftCalendarsForAccount(ctx context.Context, accountID string) ([]MicrosoftCalendarChoice, error) {
	if accountID == "" {
		return nil, errors.New("calendar: account ID required")
	}
	if a.auth == nil {
		return nil, errors.New("calendar: OAuth handle not configured")
	}

	transient := Source{Type: SourceTypeMicrosoft, AccountID: accountID}
	p := microsoftProvider{store: a.store, auth: a.auth}

	entries, err := p.ListMicrosoftCalendars(ctx, transient)
	if err != nil {
		return nil, err
	}

	out := make([]MicrosoftCalendarChoice, 0, len(entries))
	for _, e := range entries {
		out = append(out, MicrosoftCalendarChoice{
			ID:                e.ID,
			Name:              e.Name,
			IsDefaultCalendar: e.IsDefaultCalendar,
			Writable:          e.CanEdit,
		})
	}
	return out, nil
}

// AddMicrosoftSource persists a Microsoft-backed source + the user's
// chosen calendars in one transaction. Triggers an initial sync via
// bridge.go's Calendar_AddMicrosoftSource caller.
func (a *API) AddMicrosoftSource(accountID, name string, selections []MicrosoftCalendarSelection) (string, error) {
	if accountID == "" {
		return "", errors.New("calendar: account ID required")
	}
	if name == "" {
		return "", errors.New("calendar: name required")
	}
	if len(selections) == 0 {
		return "", errors.New("calendar: at least one calendar must be selected")
	}

	sourceID := uuid.New().String()
	now := time.Now().Unix()
	err := a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.CreateSourceTx(tx, Source{
			ID:              sourceID,
			Type:            SourceTypeMicrosoft,
			Name:            name,
			URL:             "",
			Username:        "",
			SyncIntervalMin: 15,
			AccountID:       accountID,
			Enabled:         true,
			Writable:        true,
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		for _, sel := range selections {
			if sel.ID == "" {
				return errors.New("calendar: selection missing calendar ID")
			}
			displayName := sel.DisplayName
			if displayName == "" {
				displayName = sel.ID
			}
			if err := a.store.CreateCalendarTx(tx, Calendar{
				ID:          uuid.New().String(),
				SourceID:    sourceID,
				URL:         sel.ID,
				DisplayName: displayName,
				Color:       sel.Color,
				Visible:     true,
				Writable:    sel.Writable,
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("persist microsoft source: %w", err)
	}
	return sourceID, nil
}

// AddGoogleSource persists a Google-backed source + the user's chosen
// calendars in one transaction, then triggers an initial sync (caller's
// responsibility — bridge wires it).
func (a *API) AddGoogleSource(accountID, name string, selections []GoogleCalendarSelection) (string, error) {
	if accountID == "" {
		return "", errors.New("calendar: account ID required")
	}
	if name == "" {
		return "", errors.New("calendar: name required")
	}
	if len(selections) == 0 {
		return "", errors.New("calendar: at least one calendar must be selected")
	}

	sourceID := uuid.New().String()
	now := time.Now().Unix()
	err := a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.CreateSourceTx(tx, Source{
			ID:              sourceID,
			Type:            SourceTypeGoogle,
			Name:            name,
			URL:             "", // Google sources have no single endpoint URL
			Username:        "", // OAuth-only; no username
			SyncIntervalMin: 15,
			AccountID:       accountID,
			Enabled:         true,
			Writable:        true, // CanWrite from googleProvider.Capabilities
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		for _, sel := range selections {
			if sel.ID == "" {
				return errors.New("calendar: selection missing calendar ID")
			}
			displayName := sel.DisplayName
			if displayName == "" {
				displayName = sel.ID
			}
			if err := a.store.CreateCalendarTx(tx, Calendar{
				ID:          uuid.New().String(),
				SourceID:    sourceID,
				URL:         sel.ID, // store Google's calendarId in the url column
				DisplayName: displayName,
				Color:       sel.Color,
				Visible:     true,
				Writable:    sel.Writable,
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("persist google source: %w", err)
	}
	return sourceID, nil
}
