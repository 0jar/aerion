package backend

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	extcaldav "github.com/emersion/go-webdav/caldav"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// Syncer drives periodic CalDAV sync for the calendar extension. Per
// source: one goroutine + one ticker at the source's sync_interval_min.
// Subscribes to `system:wake` and `system:network-online` on the host
// EventBus to trigger immediate sync after resume from sleep / network
// recovery. Errors are published to `calendar:source-error` for the
// frontend to surface.
//
// Lazy + isolated per the extension architecture: constructed inside
// CalendarBridge.ensureInit on first enabled bridge call. Disabled
// extensions never instantiate the Syncer.
type Syncer struct {
	store    *Store
	secrets  coreapi.Secrets
	events   coreapi.EventBus
	settings SettingsStore

	mu            sync.Mutex
	sourceCancels map[string]context.CancelFunc // sourceID → cancel for its goroutine
	parentCtx     context.Context
	parentCancel  context.CancelFunc

	// busySources prevents overlapping syncs on the same source — if a
	// system:wake fires while a tick is still running, we drop the wake
	// trigger for that source rather than queuing.
	busy   map[string]bool
	busyMu sync.Mutex

	unsubs []coreapi.Unsubscribe
}

// NewSyncer constructs a Syncer. Caller must call Start to begin the
// per-source goroutines + event subscriptions.
func NewSyncer(store *Store, secrets coreapi.Secrets, events coreapi.EventBus, settings SettingsStore) *Syncer {
	return &Syncer{
		store:         store,
		secrets:       secrets,
		events:        events,
		settings:      settings,
		sourceCancels: make(map[string]context.CancelFunc),
		busy:          make(map[string]bool),
	}
}

// Start spawns a goroutine for each configured source + subscribes to
// system wake/network events. Safe to call multiple times — second call
// is effectively a no-op since the underlying state is per-source.
//
// Returns the parent context's cancel func so the caller (typically the
// extension bridge) can shut down all goroutines together if it ever
// implements clean teardown. (Currently the lifecycle pattern is "leave
// them running until process exit," matching contacts.)
func (s *Syncer) Start() context.CancelFunc {
	s.mu.Lock()
	if s.parentCtx == nil {
		s.parentCtx, s.parentCancel = context.WithCancel(context.Background())
	}
	s.mu.Unlock()

	sources, err := s.store.ListSources()
	if err == nil {
		for _, src := range sources {
			s.startSourceLoop(src.ID, time.Duration(src.SyncIntervalMin)*time.Minute)
		}
	}

	s.subscribeOnce()

	// Fire an initial sync of all sources in the background so newly-
	// opened calendars populate without waiting for the first tick.
	go func() { _ = s.SyncAllSources(s.parentCtx) }()

	return s.parentCancel
}

// AddSource starts a sync goroutine for a newly added source. Called by
// the bridge after Calendar_AddCalDAVSource succeeds.
func (s *Syncer) AddSource(sourceID string, intervalMin int) {
	if intervalMin <= 0 {
		intervalMin = 15
	}
	s.startSourceLoop(sourceID, time.Duration(intervalMin)*time.Minute)
	go func() {
		ctx, cancel := context.WithTimeout(s.parentCtx, 2*time.Minute)
		defer cancel()
		_ = s.SyncSource(ctx, sourceID)
	}()
}

// RemoveSource cancels the goroutine for a deleted source. Called by the
// bridge after Calendar_DeleteSource. Idempotent.
func (s *Syncer) RemoveSource(sourceID string) {
	s.mu.Lock()
	if cancel, ok := s.sourceCancels[sourceID]; ok {
		cancel()
		delete(s.sourceCancels, sourceID)
	}
	s.mu.Unlock()
}

// SyncAllSources runs SyncSource for every configured source sequentially.
// Used for app-startup + wake/network triggers. Errors per-source are
// emitted via events and stored on the source row; the loop continues
// past individual failures.
func (s *Syncer) SyncAllSources(ctx context.Context) error {
	sources, err := s.store.ListSources()
	if err != nil {
		return fmt.Errorf("list sources for full sync: %w", err)
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.SyncSource(ctx, src.ID)
	}
	return nil
}

// SyncSource runs one sync pass against the given source. Returns the
// first error encountered; also persists it on the source row + emits
// a `calendar:source-error` event. Gate-checks via settings.IsExtensionEnabled
// — a disabled extension that still has a running ticker (because the
// user toggled disable mid-session) becomes a no-op.
func (s *Syncer) SyncSource(ctx context.Context, sourceID string) error {
	if s.settings != nil {
		enabled, _ := s.settings.IsExtensionEnabled(extensionID)
		if !enabled {
			return nil
		}
	}

	if !s.tryAcquireBusy(sourceID) {
		return nil // already syncing this source; skip
	}
	defer s.releaseBusy(sourceID)

	if err := s.syncSourceInner(ctx, sourceID); err != nil {
		_ = s.store.UpdateSourceSyncStatus(sourceID, err.Error())
		_ = s.events.Publish("calendar:source-error", map[string]any{
			"sourceId": sourceID,
			"message":  err.Error(),
		})
		return err
	}
	_ = s.store.UpdateSourceSyncStatus(sourceID, "")
	// Notify subscribers that this source finished syncing — the frontend's
	// events store refetches its window cache so newly-arrived events
	// render without waiting for the next view-state change.
	_ = s.events.Publish("calendar:sync-complete", map[string]any{
		"sourceId": sourceID,
	})
	return nil
}

func (s *Syncer) syncSourceInner(ctx context.Context, sourceID string) error {
	src, err := s.store.GetSource(sourceID)
	if err != nil {
		return fmt.Errorf("load source: %w", err)
	}
	if src.Type != "caldav" {
		// Phase 2 will add 'google' / 'microsoft' types.
		return nil
	}

	password, err := s.secrets.Get(sourceID)
	if err != nil {
		return fmt.Errorf("load password: %w", err)
	}
	if password == "" {
		return fmt.Errorf("no password stored for source — re-add it in settings")
	}

	httpClient := webdav.HTTPClientWithBasicAuth(
		newCalDAVSyncHTTPClient(60*time.Second),
		src.Username, password,
	)
	client, err := extcaldav.NewClient(httpClient, src.URL)
	if err != nil {
		return fmt.Errorf("new caldav client: %w", err)
	}

	calendars, err := s.store.ListCalendars(sourceID)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}

	for _, cal := range calendars {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.syncCalendar(ctx, client, cal); err != nil {
			// One bad calendar shouldn't block the rest; log via the
			// source-error event but keep going.
			_ = s.events.Publish("calendar:source-error", map[string]any{
				"sourceId":   sourceID,
				"calendarId": cal.ID,
				"message":    err.Error(),
			})
		}
	}
	return nil
}

func (s *Syncer) syncCalendar(ctx context.Context, client *extcaldav.Client, cal Calendar) error {
	query := &extcaldav.CalendarQuery{
		CompRequest: extcaldav.CalendarCompRequest{
			Name:     "VCALENDAR",
			AllProps: true,
			AllComps: true,
		},
		CompFilter: extcaldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []extcaldav.CompFilter{
				{Name: "VEVENT"},
			},
		},
	}

	objects, err := client.QueryCalendar(ctx, cal.URL, query)
	if err != nil {
		return fmt.Errorf("query calendar %q: %w", cal.DisplayName, err)
	}

	// Index server response by UID for diff against local. Also retain
	// the parsed ParsedObject + ETag + href so we can upsert in one pass.
	type serverEntry struct {
		etag    string
		href    string
		parsed  *ParsedObject
		rawICS  string
	}
	server := make(map[string]serverEntry, len(objects))
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		rawICS, encErr := encodeICS(obj.Data)
		if encErr != nil {
			// Skip malformed objects rather than abort the whole sync.
			continue
		}
		parsed, perr := ParseCalendarObject(rawICS)
		if perr != nil {
			continue
		}
		server[parsed.Master.UID] = serverEntry{
			etag:   obj.ETag,
			href:   obj.Path,
			parsed: parsed,
			rawICS: rawICS,
		}
	}

	// Local snapshot.
	localETags, err := s.store.ListEventETags(cal.ID)
	if err != nil {
		return fmt.Errorf("list local etags: %w", err)
	}

	return s.store.WithTx(func(tx *sql.Tx) error {
		// Upsert NEW + CHANGED.
		for uid, srv := range server {
			localETag, exists := localETags[uid]
			if exists && localETag == srv.etag && srv.etag != "" {
				continue // unchanged; skip
			}

			// New row → fresh UUID. Existing row → keep its ID by relying
			// on the UNIQUE(calendar_id, uid) constraint + ON CONFLICT
			// behavior in UpsertEventTx.
			eventID := uuid.New().String()
			if exists {
				if existing, err := s.lookupEventIDByUID(cal.ID, uid); err == nil && existing != "" {
					eventID = existing
				}
			}

			ev := srv.parsed.Master
			ev.ID = eventID
			ev.CalendarID = cal.ID
			ev.ETag = srv.etag
			ev.Href = srv.href

			if err := s.store.UpsertEventTx(tx, ev); err != nil {
				return err
			}

			// Re-write overrides for this event. Simpler than diffing —
			// the override count is typically small. Delete existing
			// overrides first, then insert new ones from the parsed
			// object. (No DeleteOverrides helper yet; inline the SQL.)
			if _, err := tx.Exec(
				`DELETE FROM event_recurrence_overrides WHERE event_id = ?`,
				eventID,
			); err != nil {
				return fmt.Errorf("clear old overrides: %w", err)
			}
			for _, ov := range srv.parsed.Overrides {
				if err := s.store.UpsertOverrideTx(tx, eventID, ov.RecurrenceIDUnix, ov.ICSBlob); err != nil {
					return err
				}
			}
		}

		// Delete events that disappeared from the server.
		for uid := range localETags {
			if _, stillOnServer := server[uid]; stillOnServer {
				continue
			}
			if err := s.store.DeleteEventByUIDTx(tx, cal.ID, uid); err != nil {
				return err
			}
		}

		// Update calendar's last_synced_at. Ctag stays NULL for now —
		// the v1 sync ignores it.
		return s.store.UpdateCalendarCtagTx(tx, cal.ID, "", time.Now().Unix())
	})
}

func (s *Syncer) lookupEventIDByUID(calendarID, uid string) (string, error) {
	var id string
	err := s.store.DB().QueryRow(
		`SELECT id FROM events WHERE calendar_id = ? AND uid = ?`,
		calendarID, uid,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// startSourceLoop starts (or replaces) the per-source ticker goroutine.
func (s *Syncer) startSourceLoop(sourceID string, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}

	s.mu.Lock()
	if prev, exists := s.sourceCancels[sourceID]; exists {
		prev()
		delete(s.sourceCancels, sourceID)
	}
	ctx, cancel := context.WithCancel(s.parentCtx)
	s.sourceCancels[sourceID] = cancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncCtx, syncCancel := context.WithTimeout(ctx, 2*time.Minute)
				_ = s.SyncSource(syncCtx, sourceID)
				syncCancel()
			}
		}
	}()
}

// subscribeOnce wires the system event handlers exactly once. Idempotent.
var subscribeOnceGuard sync.Once

func (s *Syncer) subscribeOnce() {
	subscribeOnceGuard.Do(func() {
		wakeUnsub, _ := s.events.Subscribe("system:wake", func(_ any) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = s.SyncAllSources(ctx)
		})
		netUnsub, _ := s.events.Subscribe("system:network-online", func(_ any) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = s.SyncAllSources(ctx)
		})
		s.unsubs = append(s.unsubs, wakeUnsub, netUnsub)
	})
}

func (s *Syncer) tryAcquireBusy(sourceID string) bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if s.busy[sourceID] {
		return false
	}
	s.busy[sourceID] = true
	return true
}

func (s *Syncer) releaseBusy(sourceID string) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	delete(s.busy, sourceID)
}

// encodeICS re-encodes a parsed ical.Calendar back to ICS text. Needed
// because we want the raw ICS blob stored on the events row for later
// re-parse (recurrence expansion). The go-webdav library hands us a
// parsed *ical.Calendar; we re-encode for storage.
func encodeICS(cal *ical.Calendar) (string, error) {
	if cal == nil {
		return "", fmt.Errorf("encodeICS: nil calendar")
	}
	var sb strings.Builder
	enc := ical.NewEncoder(&sb)
	if err := enc.Encode(cal); err != nil {
		return "", fmt.Errorf("encodeICS: %w", err)
	}
	return sb.String(), nil
}
