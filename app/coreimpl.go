package app

import (
	"fmt"
	"net/http"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// coreImpl is the host-side implementation of coreapi.Core handed to each
// extension during its lifecycle Register call. It exposes the existing App
// dependencies (mailAPI, composerAPI, contactsAPI, authBroker, uiRegistry)
// through the v1 interfaces.
//
// One coreImpl is constructed PER extension at App.Startup. The extensionID
// field scopes Auth() to that specific extension so the Auth Broker can route
// HTTPClient requests via the extension's own client config (or via Aerion
// core's mail OAuth, per the manifest's first_party_uses_core_for_scopes).
//
// Storage, Notifications, and Events are still Phase 1 stubs.
type coreImpl struct {
	app         *App
	extensionID string
	manifest    coreapi.Manifest
}

// newCoreForExtension constructs a coreImpl scoped to the given extension.
// Safe to call after App.Startup has constructed the underlying APIs.
func newCoreForExtension(a *App, ext coreapi.Extension) *coreImpl {
	m := ext.Manifest()
	return &coreImpl{
		app:         a,
		extensionID: m.ID,
		manifest:    m,
	}
}

func (c *coreImpl) Mail() coreapi.Mail         { return c.app.mailAPI }
func (c *coreImpl) Composer() coreapi.Composer { return c.app.composerAPI }

// Contacts returns a coreapi.Contacts surface that exposes source management
// (ListSources, LinkAccountSource) to extensions. The contact CRUD methods
// (Search/Get/List/Create/Update/Delete) are still owned by the Contacts
// extension's Bridge (extensions/contacts/backend/bridge.go) and remain
// ErrUnimplemented at this host-level surface — no cross-extension consumer
// queries those yet, and routing them through coreImpl would force the
// Contacts extension to initialize even when disabled, breaking the
// lightweight-by-default invariant. Source management lives here (rather
// than on the Bridge) because contact_sources is a host-owned table and
// the host has the full source CRUD already; the extension just needs a
// read-and-link surface to drive its sidebar + account-setup hook.
func (c *coreImpl) Contacts() coreapi.Contacts {
	return contactsCoreImpl{app: c.app}
}

type contactsCoreImpl struct {
	app *App
}

func (contactsCoreImpl) SearchContacts(string, int) ([]coreapi.Contact, error) {
	return nil, coreapi.ErrUnimplemented
}
func (contactsCoreImpl) GetContact(string) (*coreapi.Contact, error) {
	return nil, coreapi.ErrUnimplemented
}
func (contactsCoreImpl) ListContacts(coreapi.ContactFilter) ([]coreapi.Contact, error) {
	return nil, coreapi.ErrUnimplemented
}
func (contactsCoreImpl) ListAddressbooks(string) ([]coreapi.Addressbook, error) {
	return nil, coreapi.ErrUnimplemented
}

// ListSources wraps the host's existing contact-source store. Filters down
// to the API-surface shape (ContactSource) so the extension only sees what
// it consumes — id, name, type, writable.
func (c contactsCoreImpl) ListSources() ([]coreapi.ContactSource, error) {
	sources, err := c.app.carddavStore.ListSources()
	if err != nil {
		return nil, err
	}
	out := make([]coreapi.ContactSource, 0, len(sources))
	for _, s := range sources {
		if s == nil {
			continue
		}
		out = append(out, coreapi.ContactSource{
			ID:       s.ID,
			Name:     s.Name,
			Type:     string(s.Type),
			Writable: s.Writable,
		})
	}
	return out, nil
}

// LinkAccountSource delegates to the host's existing LinkAccountContactSource
// Wails method. Returns the new source's id (Wails method returned the full
// source struct; we extract its ID since that's all the extension needs).
func (c contactsCoreImpl) LinkAccountSource(accountID, name string, syncInterval int) (string, error) {
	source, err := c.app.LinkAccountContactSource(accountID, name, syncInterval)
	if err != nil {
		return "", err
	}
	if source == nil {
		return "", nil
	}
	return source.ID, nil
}

func (contactsCoreImpl) CreateContact(coreapi.ContactCreateInput) (string, error) {
	return "", coreapi.ErrUnimplemented
}
func (contactsCoreImpl) UpdateContact(string, coreapi.ContactPatch) error {
	return coreapi.ErrUnimplemented
}
func (contactsCoreImpl) DeleteContact(string) error { return coreapi.ErrUnimplemented }
func (contactsCoreImpl) SubscribeToContactEvents([]coreapi.ContactEventType) (<-chan coreapi.ContactEvent, coreapi.Unsubscribe, error) {
	return nil, func() {}, coreapi.ErrUnimplemented
}
func (c *coreImpl) Auth() coreapi.Auth {
	return &extensionAuth{
		app:         c.app,
		extensionID: c.extensionID,
		manifest:    c.manifest,
	}
}
func (c *coreImpl) UI() coreapi.UI                       { return c.app.uiRegistry }
func (c *coreImpl) Notifications() coreapi.Notifications { return stubNotifications{} }
func (c *coreImpl) Storage() coreapi.Storage             { return stubStorage{} }
func (c *coreImpl) Events() coreapi.EventBus             { return stubEventBus{} }

// Extension returns the typed handle published by another extension via its
// api.go, or (nil, false) if the extension is not loaded.
func (c *coreImpl) Extension(id string) (any, bool) {
	return nil, false
}

// --- Per-extension Auth wrapper --------------------------------------------
//
// extensionAuth bundles the calling extension's identity + manifest with the
// shared Auth Broker. HTTPClient consults the manifest's
// first_party_uses_core_for_scopes to decide whether each scope routes through
// Aerion core's mail OAuth (<provider>-mail) or the extension's own client
// config (<provider>-<extensionID>). Mixed-scope calls are rejected; the
// extension must issue separate HTTPClient calls for each routing target.

type extensionAuth struct {
	app         *App
	extensionID string
	manifest    coreapi.Manifest
}

func (a *extensionAuth) HTTPClient(accountID string, scopes []coreapi.AuthScope) (*http.Client, error) {
	return a.app.authBroker.HTTPClientForExtension(a.extensionID, a.manifest, accountID, scopes)
}

func (a *extensionAuth) IMAPClient(accountID string, requiredCaps []string) (coreapi.IMAPClient, error) {
	// IMAP via broker isn't wired yet (Phase 2+). Mail uses imap.Pool directly.
	return a.app.authBroker.IMAPClient(accountID, requiredCaps)
}

func (a *extensionAuth) SMTPClient(accountID string) (coreapi.SMTPClient, error) {
	return a.app.authBroker.SMTPClient(accountID)
}

// --- Phase 1 stubs for unimplemented surfaces -------------------------------

type stubNotifications struct{}

func (stubNotifications) Show(req coreapi.NotifyRequest) error {
	return coreapi.ErrUnimplemented
}

type stubStorage struct{}

func (stubStorage) KV(extensionID string) coreapi.KVStore {
	return stubKV{extensionID: extensionID}
}

type stubKV struct {
	extensionID string
}

func (k stubKV) Get(key string) (string, error) {
	return "", fmt.Errorf("storage.KV: extension %q has no host-provided KV in Phase 2a (use its own Store directly)", k.extensionID)
}
func (k stubKV) Set(key, value string) error          { return coreapi.ErrUnimplemented }
func (k stubKV) Delete(key string) error              { return coreapi.ErrUnimplemented }
func (k stubKV) List(prefix string) ([]string, error) { return nil, coreapi.ErrUnimplemented }

type stubEventBus struct{}

func (stubEventBus) Publish(name string, payload any) error {
	return coreapi.ErrUnimplemented
}

func (stubEventBus) Subscribe(name string, handler func(payload any)) (coreapi.Unsubscribe, error) {
	return nil, coreapi.ErrUnimplemented
}

// compile-time check: coreImpl satisfies coreapi.Core, extensionAuth satisfies coreapi.Auth
var _ coreapi.Core = (*coreImpl)(nil)
var _ coreapi.Auth = (*extensionAuth)(nil)
