package backend

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/extensions"
)

// migrations is the per-extension migration sequence for the Calendar
// extension's isolated DB. Each entry runs in version order, idempotent.
var migrations = []extensions.Migration{
	{
		Version: 1,
		SQL: `
			-- Phase 1A placeholder. Kept in place so deployments that ran 1A
			-- before 1B don't see an empty schema-bookkeeping table. Inert
			-- otherwise.
			CREATE TABLE IF NOT EXISTS meta (
				key        TEXT PRIMARY KEY,
				value      TEXT NOT NULL,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
		`,
	},
	{
		Version: 2,
		SQL: `
			-- Phase 1B: real CalDAV source + calendars schema.
			--
			-- calendar_sources: one row per CalDAV server the user has
			-- connected. Password is NOT stored here — it lives in
			-- coreapi.Storage.Secrets (keyring-first with encrypted-DB
			-- fallback in core's extension_secrets table).
			--
			-- calendars: one row per calendar within a source. CASCADE on
			-- source delete; the source row is the only handle the user has
			-- on this data. ctag + last_synced_at populated by 1C sync.

			CREATE TABLE IF NOT EXISTS calendar_sources (
				id                  TEXT PRIMARY KEY,
				type                TEXT NOT NULL,
				name                TEXT NOT NULL,
				url                 TEXT NOT NULL,
				username            TEXT NOT NULL,
				sync_interval_min   INTEGER NOT NULL DEFAULT 15,
				last_synced_at      INTEGER,
				last_error          TEXT,
				last_error_at       INTEGER,
				account_id          TEXT,
				enabled             INTEGER NOT NULL DEFAULT 1,
				created_at          INTEGER NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_calendar_sources_type ON calendar_sources(type);

			CREATE TABLE IF NOT EXISTS calendars (
				id              TEXT PRIMARY KEY,
				source_id       TEXT NOT NULL REFERENCES calendar_sources(id) ON DELETE CASCADE,
				url             TEXT NOT NULL,
				display_name    TEXT NOT NULL,
				description     TEXT,
				color           TEXT,
				visible         INTEGER NOT NULL DEFAULT 1,
				ctag            TEXT,
				last_synced_at  INTEGER,
				created_at      INTEGER NOT NULL,
				UNIQUE(source_id, url)
			);
			CREATE INDEX IF NOT EXISTS idx_calendars_source ON calendars(source_id);
		`,
	},
	{
		Version: 3,
		SQL: `
			-- Phase 1C: events + RECURRENCE-ID overrides + sync log.
			--
			-- events.ics_blob is the source of truth for recurrence expansion;
			-- rrule_text is denormalized as a query convenience (NULL = non-
			-- recurring). dtstart_unix is in epoch seconds; tz_name is the
			-- IANA tz the original VEVENT used (empty = floating or UTC).
			-- Recurring events are stored as ONE row per UID with their RRULE;
			-- per-instance overrides land in event_recurrence_overrides.

			CREATE TABLE IF NOT EXISTS events (
				id              TEXT PRIMARY KEY,
				calendar_id     TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
				uid             TEXT NOT NULL,
				etag            TEXT NOT NULL,
				href            TEXT NOT NULL,
				summary         TEXT NOT NULL,
				description     TEXT,
				location        TEXT,
				dtstart_unix    INTEGER NOT NULL,
				dtend_unix      INTEGER NOT NULL,
				is_all_day      INTEGER NOT NULL DEFAULT 0,
				tz_name         TEXT,
				rrule_text      TEXT,
				ics_blob        TEXT NOT NULL,
				UNIQUE(calendar_id, uid)
			);
			CREATE INDEX IF NOT EXISTS idx_events_calendar ON events(calendar_id);
			CREATE INDEX IF NOT EXISTS idx_events_dtstart ON events(dtstart_unix);

			CREATE TABLE IF NOT EXISTS event_recurrence_overrides (
				event_id            TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
				recurrence_id_unix  INTEGER NOT NULL,
				ics_blob            TEXT NOT NULL,
				PRIMARY KEY (event_id, recurrence_id_unix)
			);

			CREATE TABLE IF NOT EXISTS sync_log (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				source_id    TEXT,
				started_at   INTEGER NOT NULL,
				finished_at  INTEGER,
				status       TEXT NOT NULL,
				message      TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_sync_log_source ON sync_log(source_id);
		`,
	},
	{
		Version: 4,
		SQL: `
			-- Phase 1G: VALARM notifications.
			--
			-- One row per (event-instance × VALARM) within the scheduler's 24h
			-- horizon. instance_unix is the occurrence DTSTART; trigger_unix is
			-- the resolved fire-time. status flows pending → fired | dismissed.
			-- The unique index makes per-sync re-evaluation idempotent.

			CREATE TABLE IF NOT EXISTS event_alarms (
				id                  TEXT PRIMARY KEY,
				event_id            TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
				instance_unix       INTEGER NOT NULL,
				trigger_unix        INTEGER NOT NULL,
				status              TEXT NOT NULL DEFAULT 'pending',
				action              TEXT NOT NULL DEFAULT 'display',
				description         TEXT,
				fired_at            INTEGER,
				created_at          INTEGER NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_event_alarms_status ON event_alarms(status);
			CREATE INDEX IF NOT EXISTS idx_event_alarms_trigger ON event_alarms(trigger_unix);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_event_alarms_unique
				ON event_alarms(event_id, instance_unix, trigger_unix);
		`,
	},
}

// Store wraps the per-extension DB for the Calendar extension. Lives in an
// isolated SQLite file at <dataDir>/extensions/calendar/data.db, separate
// from Aerion's main DB. No tables in this file are read or written by
// core code; cross-extension access (none exists yet for Calendar) flows
// through coreapi only.
type Store struct {
	*extensions.Store
}

// NewStore opens the Calendar extension's isolated SQLite DB and applies
// pending migrations. Called eagerly from App.Startup whether or not the
// extension is enabled — keeps the schema valid across enable/disable
// cycles. The same pattern Contacts uses.
func NewStore(dataDir string) (*Store, error) {
	s, err := extensions.OpenStore(dataDir, "calendar", migrations)
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

// Source is the Go type returned by the API + Wails methods for a calendar
// source row. JSON tags drive the TS binding shape — keep stable.
type Source struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Username        string `json:"username"`
	SyncIntervalMin int    `json:"syncIntervalMin"`
	LastSyncedAt    int64  `json:"lastSyncedAt"`
	LastError       string `json:"lastError,omitempty"`
	LastErrorAt     int64  `json:"lastErrorAt,omitempty"`
	AccountID       string `json:"accountId,omitempty"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       int64  `json:"createdAt"`
}

// Calendar is the Go type for one calendar row within a source.
type Calendar struct {
	ID           string `json:"id"`
	SourceID     string `json:"sourceId"`
	URL          string `json:"url"`
	DisplayName  string `json:"displayName"`
	Description  string `json:"description,omitempty"`
	Color        string `json:"color,omitempty"`
	Visible      bool   `json:"visible"`
	Ctag         string `json:"ctag,omitempty"`
	LastSyncedAt int64  `json:"lastSyncedAt"`
	CreatedAt    int64  `json:"createdAt"`
}

// WithTx runs fn inside a SQLite transaction on the per-extension DB.
// Rolls back on any error returned from fn; commits on nil. Matches the
// transaction helper carddav.Store uses for multi-row atomic operations.
func (s *Store) WithTx(fn func(*sql.Tx) error) error {
	tx, err := s.DB().Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// CreateSourceTx inserts a calendar_sources row inside an existing
// transaction. The caller is responsible for committing.
func (s *Store) CreateSourceTx(tx *sql.Tx, src Source) error {
	if src.ID == "" {
		return errors.New("calendar.Store: source ID required")
	}
	if src.CreatedAt == 0 {
		src.CreatedAt = time.Now().Unix()
	}
	if src.SyncIntervalMin == 0 {
		src.SyncIntervalMin = 15
	}
	_, err := tx.Exec(`
		INSERT INTO calendar_sources (
			id, type, name, url, username, sync_interval_min,
			last_synced_at, last_error, last_error_at,
			account_id, enabled, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.Type, src.Name, src.URL, src.Username, src.SyncIntervalMin,
		nullIfZero(src.LastSyncedAt), nullIfEmpty(src.LastError), nullIfZero(src.LastErrorAt),
		nullIfEmpty(src.AccountID), boolToInt(src.Enabled), src.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert calendar_source: %w", err)
	}
	return nil
}

// GetSource returns one source by id, or sql.ErrNoRows when missing.
func (s *Store) GetSource(id string) (*Source, error) {
	row := s.DB().QueryRow(`
		SELECT id, type, name, url, username, sync_interval_min,
		       COALESCE(last_synced_at, 0), COALESCE(last_error, ''), COALESCE(last_error_at, 0),
		       COALESCE(account_id, ''), enabled, created_at
		FROM calendar_sources WHERE id = ?`, id)
	src := &Source{}
	var enabled int
	if err := row.Scan(
		&src.ID, &src.Type, &src.Name, &src.URL, &src.Username, &src.SyncIntervalMin,
		&src.LastSyncedAt, &src.LastError, &src.LastErrorAt,
		&src.AccountID, &enabled, &src.CreatedAt,
	); err != nil {
		return nil, err
	}
	src.Enabled = enabled != 0
	return src, nil
}

// ListSources returns all configured calendar sources ordered by created_at.
func (s *Store) ListSources() ([]Source, error) {
	rows, err := s.DB().Query(`
		SELECT id, type, name, url, username, sync_interval_min,
		       COALESCE(last_synced_at, 0), COALESCE(last_error, ''), COALESCE(last_error_at, 0),
		       COALESCE(account_id, ''), enabled, created_at
		FROM calendar_sources ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("query calendar_sources: %w", err)
	}
	defer rows.Close()

	var out []Source
	for rows.Next() {
		var src Source
		var enabled int
		if err := rows.Scan(
			&src.ID, &src.Type, &src.Name, &src.URL, &src.Username, &src.SyncIntervalMin,
			&src.LastSyncedAt, &src.LastError, &src.LastErrorAt,
			&src.AccountID, &enabled, &src.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan calendar_source: %w", err)
		}
		src.Enabled = enabled != 0
		out = append(out, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendar_sources: %w", err)
	}
	return out, nil
}

// DeleteSource removes a calendar_sources row. CASCADE removes the
// associated calendars rows. Idempotent — deleting a non-existent source
// is not an error.
func (s *Store) DeleteSource(id string) error {
	_, err := s.DB().Exec(`DELETE FROM calendar_sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete calendar_source: %w", err)
	}
	return nil
}

// CreateCalendarTx inserts a calendars row inside an existing transaction.
func (s *Store) CreateCalendarTx(tx *sql.Tx, cal Calendar) error {
	if cal.ID == "" {
		return errors.New("calendar.Store: calendar ID required")
	}
	if cal.SourceID == "" {
		return errors.New("calendar.Store: source ID required")
	}
	if cal.CreatedAt == 0 {
		cal.CreatedAt = time.Now().Unix()
	}
	_, err := tx.Exec(`
		INSERT INTO calendars (
			id, source_id, url, display_name, description, color,
			visible, ctag, last_synced_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cal.ID, cal.SourceID, cal.URL, cal.DisplayName, nullIfEmpty(cal.Description),
		nullIfEmpty(cal.Color), boolToInt(cal.Visible), nullIfEmpty(cal.Ctag),
		nullIfZero(cal.LastSyncedAt), cal.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert calendar: %w", err)
	}
	return nil
}

// ListCalendars returns all calendar rows for a source, ordered by display
// name for stable UI rendering.
func (s *Store) ListCalendars(sourceID string) ([]Calendar, error) {
	rows, err := s.DB().Query(`
		SELECT id, source_id, url, display_name,
		       COALESCE(description, ''), COALESCE(color, ''),
		       visible, COALESCE(ctag, ''), COALESCE(last_synced_at, 0), created_at
		FROM calendars WHERE source_id = ? ORDER BY display_name ASC`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query calendars: %w", err)
	}
	defer rows.Close()

	var out []Calendar
	for rows.Next() {
		var cal Calendar
		var visible int
		if err := rows.Scan(
			&cal.ID, &cal.SourceID, &cal.URL, &cal.DisplayName,
			&cal.Description, &cal.Color,
			&visible, &cal.Ctag, &cal.LastSyncedAt, &cal.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		cal.Visible = visible != 0
		out = append(out, cal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendars: %w", err)
	}
	return out, nil
}

// nullIfEmpty returns the input wrapped in sql.NullString — empty strings
// become SQL NULL so COALESCE works on read.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullIfZero returns SQL NULL for zero int64 inputs.
func nullIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// boolToInt converts a Go bool to SQLite's 0/1 integer convention.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Event types ------------------------------------------------------------

// Event is the master representation of a calendar event in storage. For
// recurring events, exactly one Event row exists per UID; per-instance
// overrides live in EventOverride. JSON tags drive the TS binding shape.
type Event struct {
	ID          string `json:"id"`
	CalendarID  string `json:"calendarId"`
	UID         string `json:"uid"`
	ETag        string `json:"etag"`
	Href        string `json:"href"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	DTStartUnix int64  `json:"dtstartUnix"`
	DTEndUnix   int64  `json:"dtendUnix"`
	IsAllDay    bool   `json:"isAllDay"`
	TZName      string `json:"tzName,omitempty"`
	RRuleText   string `json:"rruleText,omitempty"`
	ICSBlob     string `json:"-"` // not exposed to frontend; used by rrule_expand
}

// EventInstance is one occurrence of an Event in a queried time window.
// Non-recurring events produce exactly one EventInstance per Event. Recurring
// events produce zero or more (depends on the window). RECURRENCE-ID
// overrides replace the matching default-expanded instance.
type EventInstance struct {
	Event              // embed for field reuse; serialized flat in JSON
	InstanceStartUnix  int64 `json:"instanceStartUnix"`
	InstanceEndUnix    int64 `json:"instanceEndUnix"`
	IsRecurrenceOverride bool `json:"isRecurrenceOverride,omitempty"`
}

// EventOverride is one RECURRENCE-ID exception to a master recurring event.
// recurrence_id_unix matches the instance time of the occurrence being
// overridden (epoch seconds). ics_blob holds the full overriding VEVENT.
type EventOverride struct {
	EventID          string
	RecurrenceIDUnix int64
	ICSBlob          string
}

// --- Event helpers (called by sync.go + rrule_expand.go) --------------------

// UpsertEventTx inserts or replaces an events row inside a transaction.
// On conflict (calendar_id + uid), all mutable columns are updated.
func (s *Store) UpsertEventTx(tx *sql.Tx, ev Event) error {
	if ev.ID == "" {
		return errors.New("calendar.Store: event ID required")
	}
	if ev.CalendarID == "" {
		return errors.New("calendar.Store: calendar ID required")
	}
	if ev.UID == "" {
		return errors.New("calendar.Store: event UID required")
	}
	_, err := tx.Exec(`
		INSERT INTO events (
			id, calendar_id, uid, etag, href,
			summary, description, location,
			dtstart_unix, dtend_unix, is_all_day, tz_name,
			rrule_text, ics_blob
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(calendar_id, uid) DO UPDATE SET
			etag = excluded.etag,
			href = excluded.href,
			summary = excluded.summary,
			description = excluded.description,
			location = excluded.location,
			dtstart_unix = excluded.dtstart_unix,
			dtend_unix = excluded.dtend_unix,
			is_all_day = excluded.is_all_day,
			tz_name = excluded.tz_name,
			rrule_text = excluded.rrule_text,
			ics_blob = excluded.ics_blob`,
		ev.ID, ev.CalendarID, ev.UID, ev.ETag, ev.Href,
		ev.Summary, nullIfEmpty(ev.Description), nullIfEmpty(ev.Location),
		ev.DTStartUnix, ev.DTEndUnix, boolToInt(ev.IsAllDay), nullIfEmpty(ev.TZName),
		nullIfEmpty(ev.RRuleText), ev.ICSBlob,
	)
	if err != nil {
		return fmt.Errorf("upsert event: %w", err)
	}
	return nil
}

// DeleteEventByUIDTx removes an event row inside a transaction. CASCADE
// removes any associated event_recurrence_overrides.
func (s *Store) DeleteEventByUIDTx(tx *sql.Tx, calendarID, uid string) error {
	_, err := tx.Exec(`DELETE FROM events WHERE calendar_id = ? AND uid = ?`, calendarID, uid)
	if err != nil {
		return fmt.Errorf("delete event by uid: %w", err)
	}
	return nil
}

// ListEventETags returns a (uid → etag) map for one calendar. Used by sync
// to diff against the server's REPORT response. Skips events with empty
// ETag (shouldn't exist in practice; defensive).
func (s *Store) ListEventETags(calendarID string) (map[string]string, error) {
	rows, err := s.DB().Query(`SELECT uid, etag FROM events WHERE calendar_id = ?`, calendarID)
	if err != nil {
		return nil, fmt.Errorf("query event etags: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var uid, etag string
		if err := rows.Scan(&uid, &etag); err != nil {
			return nil, fmt.Errorf("scan event etag: %w", err)
		}
		if etag == "" {
			continue
		}
		out[uid] = etag
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event etags: %w", err)
	}
	return out, nil
}

// GetEvent returns a single event by ID, or sql.ErrNoRows when missing.
func (s *Store) GetEvent(id string) (*Event, error) {
	row := s.DB().QueryRow(`
		SELECT id, calendar_id, uid, etag, href,
		       summary, COALESCE(description, ''), COALESCE(location, ''),
		       dtstart_unix, dtend_unix, is_all_day, COALESCE(tz_name, ''),
		       COALESCE(rrule_text, ''), ics_blob
		FROM events WHERE id = ?`, id)
	ev := &Event{}
	var isAllDay int
	if err := row.Scan(
		&ev.ID, &ev.CalendarID, &ev.UID, &ev.ETag, &ev.Href,
		&ev.Summary, &ev.Description, &ev.Location,
		&ev.DTStartUnix, &ev.DTEndUnix, &isAllDay, &ev.TZName,
		&ev.RRuleText, &ev.ICSBlob,
	); err != nil {
		return nil, err
	}
	ev.IsAllDay = isAllDay != 0
	return ev, nil
}

// ListEventsForExpansion returns all events for the given calendars. The
// recurrence expansion step (rrule_expand) then filters by the requested
// window. v1 fetches everything — fine for typical calendars (<1k events
// each). Time-bounded fetching is a future optimization.
func (s *Store) ListEventsForExpansion(calendarIDs []string) ([]Event, error) {
	if len(calendarIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(calendarIDs))
	args := make([]any, len(calendarIDs))
	for i, id := range calendarIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`
		SELECT id, calendar_id, uid, etag, href,
		       summary, COALESCE(description, ''), COALESCE(location, ''),
		       dtstart_unix, dtend_unix, is_all_day, COALESCE(tz_name, ''),
		       COALESCE(rrule_text, ''), ics_blob
		FROM events WHERE calendar_id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events for expansion: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var ev Event
		var isAllDay int
		if err := rows.Scan(
			&ev.ID, &ev.CalendarID, &ev.UID, &ev.ETag, &ev.Href,
			&ev.Summary, &ev.Description, &ev.Location,
			&ev.DTStartUnix, &ev.DTEndUnix, &isAllDay, &ev.TZName,
			&ev.RRuleText, &ev.ICSBlob,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.IsAllDay = isAllDay != 0
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return out, nil
}

// UpsertOverrideTx inserts or replaces a RECURRENCE-ID override row.
func (s *Store) UpsertOverrideTx(tx *sql.Tx, eventID string, recurrenceIDUnix int64, blob string) error {
	_, err := tx.Exec(`
		INSERT INTO event_recurrence_overrides (event_id, recurrence_id_unix, ics_blob)
		VALUES (?, ?, ?)
		ON CONFLICT(event_id, recurrence_id_unix) DO UPDATE SET ics_blob = excluded.ics_blob`,
		eventID, recurrenceIDUnix, blob,
	)
	if err != nil {
		return fmt.Errorf("upsert override: %w", err)
	}
	return nil
}

// ListOverrides returns all RECURRENCE-ID overrides for one event.
func (s *Store) ListOverrides(eventID string) ([]EventOverride, error) {
	rows, err := s.DB().Query(`
		SELECT event_id, recurrence_id_unix, ics_blob
		FROM event_recurrence_overrides WHERE event_id = ?`, eventID)
	if err != nil {
		return nil, fmt.Errorf("query overrides: %w", err)
	}
	defer rows.Close()
	var out []EventOverride
	for rows.Next() {
		var o EventOverride
		if err := rows.Scan(&o.EventID, &o.RecurrenceIDUnix, &o.ICSBlob); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overrides: %w", err)
	}
	return out, nil
}

// UpdateCalendarCtagTx updates a calendar's ctag + last_synced_at after a
// successful sync pass.
func (s *Store) UpdateCalendarCtagTx(tx *sql.Tx, calendarID, ctag string, syncedAt int64) error {
	_, err := tx.Exec(`
		UPDATE calendars SET ctag = ?, last_synced_at = ? WHERE id = ?`,
		nullIfEmpty(ctag), nullIfZero(syncedAt), calendarID,
	)
	if err != nil {
		return fmt.Errorf("update calendar ctag: %w", err)
	}
	return nil
}

// UpdateSourceSyncStatus marks a source as synced (on success: clears
// last_error; on failure: stores the error message). Single statement; not
// inside a transaction.
func (s *Store) UpdateSourceSyncStatus(sourceID, errMsg string) error {
	now := time.Now().Unix()
	if errMsg == "" {
		_, err := s.DB().Exec(`
			UPDATE calendar_sources
			SET last_synced_at = ?, last_error = NULL, last_error_at = NULL
			WHERE id = ?`, now, sourceID)
		if err != nil {
			return fmt.Errorf("update source sync status (success): %w", err)
		}
		return nil
	}
	_, err := s.DB().Exec(`
		UPDATE calendar_sources
		SET last_error = ?, last_error_at = ?
		WHERE id = ?`, errMsg, now, sourceID)
	if err != nil {
		return fmt.Errorf("update source sync status (error): %w", err)
	}
	return nil
}

// SetCalendarVisible flips the per-calendar visibility flag.
func (s *Store) SetCalendarVisible(calendarID string, visible bool) error {
	_, err := s.DB().Exec(`UPDATE calendars SET visible = ? WHERE id = ?`,
		boolToInt(visible), calendarID)
	if err != nil {
		return fmt.Errorf("set calendar visible: %w", err)
	}
	return nil
}

// SetCalendarColor stores the user's chosen color (hex "#rrggbb") on a
// calendar. Empty string clears the override.
func (s *Store) SetCalendarColor(calendarID, hex string) error {
	_, err := s.DB().Exec(`UPDATE calendars SET color = ? WHERE id = ?`,
		nullIfEmpty(hex), calendarID)
	if err != nil {
		return fmt.Errorf("set calendar color: %w", err)
	}
	return nil
}

// UpdateSyncInterval changes the per-source poll interval. Validation
// happens at the API layer; this helper just writes.
func (s *Store) UpdateSyncInterval(sourceID string, minutes int) error {
	_, err := s.DB().Exec(`UPDATE calendar_sources SET sync_interval_min = ? WHERE id = ?`,
		minutes, sourceID)
	if err != nil {
		return fmt.Errorf("update sync interval: %w", err)
	}
	return nil
}

// --- Alarms (Phase 1G) -----------------------------------------------------

// Alarm is one VALARM instance materialized for the scheduler. Each row in
// event_alarms maps to one Alarm; recurring events produce one Alarm per
// occurrence × VALARM block.
type Alarm struct {
	ID           string `json:"id"`
	EventID      string `json:"eventId"`
	InstanceUnix int64  `json:"instanceUnix"` // DTSTART of this occurrence
	TriggerUnix  int64  `json:"triggerUnix"`  // resolved fire time (UTC seconds)
	Status       string `json:"status"`       // 'pending' | 'fired' | 'dismissed'
	Action       string `json:"action"`       // 'display' / 'audio' / 'email'
	Description  string `json:"description,omitempty"`
	FiredAt      int64  `json:"firedAt,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

// UpsertAlarmTx inserts an alarm if no row already covers the same
// (event_id, instance_unix, trigger_unix). Idempotent — safe to call
// after every sync. Existing rows are NOT updated (we don't want to
// clobber a 'fired' status by re-evaluating).
func (s *Store) UpsertAlarmTx(tx *sql.Tx, a Alarm) error {
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	if a.Status == "" {
		a.Status = "pending"
	}
	if a.Action == "" {
		a.Action = "display"
	}
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO event_alarms
			(id, event_id, instance_unix, trigger_unix, status, action, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.EventID, a.InstanceUnix, a.TriggerUnix,
		a.Status, a.Action, nullIfEmpty(a.Description), a.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert alarm: %w", err)
	}
	return nil
}

// PendingAlarmsInRange returns all 'pending' alarms with trigger_unix in
// [from, to]. Used by the scheduler to find what to arm.
func (s *Store) PendingAlarmsInRange(from, to int64) ([]Alarm, error) {
	rows, err := s.DB().Query(`
		SELECT id, event_id, instance_unix, trigger_unix, status, action,
		       COALESCE(description, ''), COALESCE(fired_at, 0), created_at
		FROM event_alarms
		WHERE status = 'pending' AND trigger_unix >= ? AND trigger_unix <= ?
		ORDER BY trigger_unix ASC`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query pending alarms: %w", err)
	}
	defer rows.Close()
	var out []Alarm
	for rows.Next() {
		var a Alarm
		if err := rows.Scan(&a.ID, &a.EventID, &a.InstanceUnix, &a.TriggerUnix,
			&a.Status, &a.Action, &a.Description, &a.FiredAt, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alarm: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAlarm reads a single alarm by ID. Returns nil + nil error when missing.
func (s *Store) GetAlarm(id string) (*Alarm, error) {
	row := s.DB().QueryRow(`
		SELECT id, event_id, instance_unix, trigger_unix, status, action,
		       COALESCE(description, ''), COALESCE(fired_at, 0), created_at
		FROM event_alarms WHERE id = ?`, id)
	var a Alarm
	err := row.Scan(&a.ID, &a.EventID, &a.InstanceUnix, &a.TriggerUnix,
		&a.Status, &a.Action, &a.Description, &a.FiredAt, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alarm: %w", err)
	}
	return &a, nil
}

// MarkAlarmFired transitions a pending alarm to 'fired' and records when.
func (s *Store) MarkAlarmFired(alarmID string, firedAt int64) error {
	_, err := s.DB().Exec(`
		UPDATE event_alarms
		SET status = 'fired', fired_at = ?
		WHERE id = ? AND status = 'pending'`, firedAt, alarmID)
	if err != nil {
		return fmt.Errorf("mark alarm fired: %w", err)
	}
	return nil
}

// MarkAlarmDismissed transitions a pending alarm to 'dismissed'.
func (s *Store) MarkAlarmDismissed(alarmID string) error {
	_, err := s.DB().Exec(`
		UPDATE event_alarms
		SET status = 'dismissed'
		WHERE id = ? AND status = 'pending'`, alarmID)
	if err != nil {
		return fmt.Errorf("mark alarm dismissed: %w", err)
	}
	return nil
}

// MarkPastAlarmsFired sweeps any pending alarms with trigger_unix in the
// past to 'fired' status without sending a notification. Used on app
// resume / startup so missed alarms don't fire-after-the-fact.
func (s *Store) MarkPastAlarmsFired(now int64) error {
	_, err := s.DB().Exec(`
		UPDATE event_alarms
		SET status = 'fired', fired_at = ?
		WHERE status = 'pending' AND trigger_unix < ?`, now, now)
	if err != nil {
		return fmt.Errorf("mark past alarms fired: %w", err)
	}
	return nil
}
