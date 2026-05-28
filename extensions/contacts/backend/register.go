package backend

import (
	"fmt"

	"github.com/hkdb/aerion/extensions/contacts"
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// Extension is the Contacts extension's Go-side handle. It owns the coreapi.Contacts
// implementation (the API wrapper) and exposes the manifest + lifecycle hooks
// that the host calls during App.Startup.
type Extension struct {
	api      *API
	store    *Store
	manifest coreapi.Manifest
}

// New constructs the Contacts extension. localStore and carddavStore are the
// host's existing core stores; the Contacts API wraps them for the
// coreapi.Contacts read surface. store is the per-extension SQLite (already
// opened by the host so its migrations apply regardless of enabled state).
func New(localStore *contact.Store, carddavStore *carddav.Store, store *Store) *Extension {
	return &Extension{
		api:      NewAPI(localStore, carddavStore),
		store:    store,
		manifest: contacts.Manifest(),
	}
}

// API returns the typed coreapi.Contacts implementation. The host can hold a
// reference to call from its Wails-bound surface; other extensions that need
// to query contacts would receive it via Core.Extension("contacts").
func (e *Extension) API() *API { return e.api }

// Store returns the per-extension SQLite wrapper for code that needs direct
// access (none in Phase 2a — the API wraps everything).
func (e *Extension) Store() *Store { return e.store }

// Manifest returns the parsed manifest embedded at build time.
func (e *Extension) Manifest() coreapi.Manifest { return e.manifest }

// Register wires the Contacts extension's UI surfaces (rail tab + account-setup
// hook). Runs once per Aerion process lifetime, at App.Startup, regardless of
// whether the extension is currently enabled — descriptive registrations
// persist across enable/disable cycles. The frontend's rail rendering and
// hook-discovery filtering live elsewhere (App.ListExtensionRailTabs filters
// by enabled state; account-setup hooks are returned regardless so they
// remain a discovery surface for disabled extensions).
//
// Returns an Unregister func that tears all registrations down. Called by
// the host on process shutdown.
func (e *Extension) Register(core coreapi.Core) (coreapi.Unregister, error) {
	unregRail, err := core.UI().RegisterRailTab(coreapi.RailTabRequest{
		ExtensionID: e.manifest.ID,
		Label:       e.manifest.Name,
		Icon:        "mdi:account-multiple",
		Component:   "ContactsPane",
		Order:       10,
	})
	if err != nil {
		return nil, fmt.Errorf("contacts: register rail tab: %w", err)
	}

	unregHook, err := core.UI().RegisterAccountSetupHook(coreapi.AccountSetupHookRequest{
		ExtensionID: e.manifest.ID,
		Providers:   []string{"google", "microsoft"},
		ButtonLabel: "Also set up your contacts",
		Description: "Sync contacts from this account for autocomplete and browsing.",
		Component:   "AccountContactsHookPanel",
	})
	if err != nil {
		unregRail()
		return nil, fmt.Errorf("contacts: register account-setup hook: %w", err)
	}

	return func() {
		unregHook()
		unregRail()
	}, nil
}

// compile-time check: *Extension satisfies coreapi.Extension
var _ coreapi.Extension = (*Extension)(nil)
