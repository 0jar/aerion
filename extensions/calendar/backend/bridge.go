package backend

import (
	"context"
	"errors"
	"sync"
	"time"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/platform"
)

// CalendarBridge is the Wails-bindable surface for the Calendar extension.
// It's embedded into the host `*app.App` struct; Go's method-promotion
// makes every CalendarBridge method appear on App so Wails' reflection-based
// bind generator picks them up. All Calendar-specific logic lives here, not
// in the host. The host's `app/extension_calendar.go` is reduced to a
// dozen lines of construction wiring.
//
// Method naming: all Wails-bound bridge methods use the `Calendar_` prefix
// so they can't collide with another extension's methods after embedding
// into the same App. See docs/EXT_RULES.md R19.
//
// Lightweight-by-default invariant: when the user has the Calendar
// extension disabled, NOTHING is loaded beyond the ~80-byte CalendarBridge
// struct itself. The per-extension SQLite is opened eagerly at Startup
// (schema-validity invariant), but the `API` wrapper that holds the
// caldav client + secrets handle is lazy-init via sync.Once inside
// `ensureInit`. The first enabled method call triggers init; subsequent
// calls are fast. See docs/EXT_RULES.md §4.
type CalendarBridge struct {
	deps CalendarBridgeDeps

	// Lazy-initialized API + Syncer. Constructed on first enabled bridge
	// call so disabled extensions contribute zero work.
	initOnce sync.Once
	initErr  error
	api      *API
	syncer   *Syncer
	alarms   *AlarmScheduler
}

// CalendarBridgeDeps bundles the host-provided dependencies the bridge needs.
// Grouped into a struct so adding a new dep doesn't churn every call site
// in the host. Per docs/EXT_RULES.md R2, this struct holds NO closures
// wrapping `internal/*` calls — anything the extension needs from the host
// goes through `coreapi.Core` directly.
type CalendarBridgeDeps struct {
	// SettingsStore is consulted on every bridge call for the enabled
	// gate (lightweight invariant — disabled calls short-circuit before
	// any work).
	SettingsStore SettingsStore

	// Paths gives the bridge access to the OS-appropriate data directory
	// for opening the extension's per-extension SQLite.
	Paths *platform.Paths

	// DB is the shared application database. Not used by the calendar
	// extension's primary data paths (calendar data lives in its own
	// per-extension SQLite, opened via Paths). Kept here for symmetry
	// with Contacts and forward-compat with Phase 2 cross-extension
	// queries that may need it.
	DB *database.DB

	// Core is the coreapi.Core handle. The bridge uses it to reach the
	// host-implemented surfaces — currently `coreapi.Storage.Secrets`
	// for the CalDAV password storage. Per-extension scoped at Core
	// construction time in `newCoreForExtension`.
	Core coreapi.Core
}

// SettingsStore is the narrow interface the bridge needs from the host's
// settings store. Defined here (rather than importing the concrete type)
// so 3rd-party extensions can swap in their own implementation for tests
// and so this file doesn't grow a host-package dependency.
type SettingsStore interface {
	IsExtensionEnabled(id string) (bool, error)
}

// NewCalendarBridge constructs the bridge with its dependencies. Does NOT
// touch the DB or open any extension state — that's the Store's job
// (called eagerly from app/extension_calendar.go to keep schema valid
// across enable/disable cycles).
func NewCalendarBridge(deps CalendarBridgeDeps) *CalendarBridge {
	return &CalendarBridge{deps: deps}
}

// extensionID is the key the bridge looks up in settings for the
// enabled-state check, AND the scope passed to coreapi.Storage.Secrets.
// Kept as a const so a typo doesn't silently disable every bridge
// method or store secrets in the wrong namespace.
const extensionID = "calendar"

// gateEnabled returns true when the extension is currently enabled AND
// the host gave us a SettingsStore. Returns false (silently) when the
// store is nil or when the settings read errors out.
func (b *CalendarBridge) gateEnabled() bool {
	if b.deps.SettingsStore == nil {
		return false
	}
	enabled, err := b.deps.SettingsStore.IsExtensionEnabled(extensionID)
	if err != nil {
		return false
	}
	return enabled
}

// ensureInit lazily constructs the API on the first enabled bridge call.
// Reuses the Store the host opened eagerly at Startup (passed via Paths
// + opened by app/extension_calendar.go). Secrets handle is fetched
// from coreapi.Core, pre-scoped to this extension's ID.
func (b *CalendarBridge) ensureInit() error {
	b.initOnce.Do(func() {
		if b.deps.DB == nil || b.deps.Paths == nil {
			b.initErr = errors.New("calendar.CalendarBridge: missing DB or Paths in deps")
			return
		}
		if b.deps.Core == nil {
			b.initErr = errors.New("calendar.CalendarBridge: missing Core in deps")
			return
		}

		store, err := NewStore(b.deps.Paths.Data)
		if err != nil {
			b.initErr = err
			return
		}

		secrets := b.deps.Core.Storage().Secrets(extensionID)
		b.api = NewAPI(store, secrets)
		b.syncer = NewSyncer(store, secrets, b.deps.Core.Events(), b.deps.SettingsStore)
		b.syncer.Start()
		b.alarms = NewAlarmScheduler(store, b.deps.Core.Notifications(), b.deps.Core.Events(), b.deps.Core.Log())
		b.alarms.Start(context.Background())
	})
	return b.initErr
}

// --- Wails-bound surface (Calendar_*) ----------------------------------------
//
// All methods gate on gateEnabled() so disabled extensions short-circuit
// before any work. ensureInit runs once per process; subsequent calls are
// the cost of one sync.Once.Done() check.

// Calendar_AddCalDAVSource probes the user-entered server URL with the
// supplied credentials, persists the source + discovered calendars, and
// stores the password via coreapi.Storage.Secrets. Returns the new
// source's ID, or an error describing where discovery failed (auth /
// principal / home-set / list).
func (b *CalendarBridge) Calendar_AddCalDAVSource(name, url, username, password string) (string, error) {
	if !b.gateEnabled() {
		return "", errors.New("calendar: extension disabled")
	}
	if err := b.ensureInit(); err != nil {
		return "", err
	}
	sourceID, err := b.api.AddCalDAVSource(name, url, username, password)
	if err != nil {
		return "", err
	}
	// Hook the new source into the periodic sync ticker + fire an
	// immediate sync in the background so events show up without waiting.
	b.syncer.AddSource(sourceID, 15)
	return sourceID, nil
}

// Calendar_ListSources returns all configured calendar sources. Returns
// nil (empty result) when the extension is disabled — consistent with
// Contacts_ListSources's behavior.
func (b *CalendarBridge) Calendar_ListSources() ([]Source, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return b.api.ListSources()
}

// Calendar_ListCalendars returns the calendars for a single source.
// Returns nil when the extension is disabled.
func (b *CalendarBridge) Calendar_ListCalendars(sourceID string) ([]Calendar, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return b.api.ListCalendars(sourceID)
}

// Calendar_DeleteSource removes a calendar source and all its associated
// data (calendars via CASCADE, stored password via coreapi.Secrets).
// Idempotent — deleting a non-existent source is not an error.
func (b *CalendarBridge) Calendar_DeleteSource(sourceID string) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	b.syncer.RemoveSource(sourceID)
	return b.api.DeleteSource(sourceID)
}

// Calendar_SyncSource runs a single sync pass against the given source.
// Returns when the sync finishes (success or failure). Errors are also
// persisted on the source row + published via `calendar:source-error`.
func (b *CalendarBridge) Calendar_SyncSource(sourceID string) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return b.syncer.SyncSource(ctx, sourceID)
}

// Calendar_SyncAllSources runs a sync pass against every configured
// source sequentially. Per-source failures don't abort the loop.
func (b *CalendarBridge) Calendar_SyncAllSources() error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return b.syncer.SyncAllSources(ctx)
}

// Calendar_ListEventsInRange is the workhorse query for calendar views.
// Expands recurring events into concrete occurrences within [fromUnix,
// toUnix]. Honors per-calendar visibility (invisible calendars are
// skipped). Result sorted by InstanceStartUnix.
func (b *CalendarBridge) Calendar_ListEventsInRange(calendarIDs []string, fromUnix, toUnix int64) ([]EventInstance, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	if len(calendarIDs) == 0 {
		return nil, nil
	}

	from := time.Unix(fromUnix, 0)
	to := time.Unix(toUnix, 0)

	// Filter to visible calendars. We could query the visible flag in
	// SQL, but the row count is small enough that doing it in Go keeps
	// the SQL simple.
	visible := make([]string, 0, len(calendarIDs))
	for _, sourceID := range listAllSourceIDs(b.api) {
		cals, _ := b.api.ListCalendars(sourceID)
		for _, cal := range cals {
			if !cal.Visible {
				continue
			}
			for _, want := range calendarIDs {
				if cal.ID == want {
					visible = append(visible, cal.ID)
					break
				}
			}
		}
	}
	if len(visible) == 0 {
		return nil, nil
	}

	events, err := b.api.store.ListEventsForExpansion(visible)
	if err != nil {
		return nil, err
	}

	var out []EventInstance
	for _, ev := range events {
		overrides, _ := b.api.store.ListOverrides(ev.ID)
		instances, err := ExpandInRange(ev, overrides, from, to)
		if err != nil {
			// Skip the bad event rather than aborting the whole query.
			continue
		}
		out = append(out, instances...)
	}
	return out, nil
}

// Calendar_GetEvent returns one event by ID. Used by the detail overlay
// (Phase 1E) and other "show me this specific event" surfaces.
func (b *CalendarBridge) Calendar_GetEvent(eventID string) (*Event, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return b.api.store.GetEvent(eventID)
}

// Calendar_SetCalendarVisible toggles a calendar's visibility in the UI.
// Cached events stay in the store; ListEventsInRange filters them out.
func (b *CalendarBridge) Calendar_SetCalendarVisible(calendarID string, visible bool) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	return b.api.store.SetCalendarVisible(calendarID, visible)
}

// Calendar_SetCalendarColor stores a hex color (`#rrggbb`) for a calendar.
// Empty string clears the override.
func (b *CalendarBridge) Calendar_SetCalendarColor(calendarID, hex string) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	return b.api.store.SetCalendarColor(calendarID, hex)
}

// Calendar_SetSyncInterval changes a source's poll interval (minutes).
// Validates {5, 15, 30, 60, 120, 240, 720}; rejects other values.
func (b *CalendarBridge) Calendar_SetSyncInterval(sourceID string, minutes int) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	if err := b.api.SetSyncInterval(sourceID, minutes); err != nil {
		return err
	}
	// Restart the per-source goroutine at the new interval so it takes
	// effect immediately rather than waiting for the next tick.
	b.syncer.UpdateInterval(sourceID, minutes)
	return nil
}

// Calendar_DismissAlarm marks a pending alarm as dismissed. Idempotent
// for already-fired/dismissed rows. The frontend doesn't surface a
// dismiss button yet, but the method exists for future UI + scripting.
func (b *CalendarBridge) Calendar_DismissAlarm(alarmID string) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	return b.api.store.MarkAlarmDismissed(alarmID)
}

// Calendar_LogFrontend emits a log message from the calendar extension's
// frontend through the host's coreapi.Logger, stamped with
// extension=calendar. The calendar-side `frontend/lib/logger.ts` wraps
// this so calendar components can call `logger.warn(msg)` without reaching
// for the host's generic LogFrontend method.
//
// Unlike most Calendar_* methods, this is NOT gated by the enabled flag —
// disabled extensions may still need to log construction-time errors. The
// extension tag in coreapi.Logger keeps disabled-extension noise easy to
// filter downstream.
func (b *CalendarBridge) Calendar_LogFrontend(level, message string) {
	if b.deps.Core == nil {
		return
	}
	log := b.deps.Core.Log()
	if log == nil {
		return
	}
	switch level {
	case "debug":
		log.Debug(message)
	case "warn":
		log.Warn(message)
	case "error":
		log.Error(message)
	default:
		log.Info(message)
	}
}

// listAllSourceIDs is a tiny helper for Calendar_ListEventsInRange —
// flattens "all sources' calendars" so we can intersect with the
// caller's requested calendar IDs. Cheap because source count is small
// in practice (<10 typical).
func listAllSourceIDs(a *API) []string {
	srcs, err := a.ListSources()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.ID)
	}
	return out
}
