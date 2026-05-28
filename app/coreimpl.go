package app

import (
	"fmt"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// coreImpl is the host-side implementation of coreapi.Core handed to each
// extension during its lifecycle Register call. It exposes the existing App
// dependencies (mailAPI, composerAPI, contactsAPI, authBroker, uiRegistry)
// through the v1 interfaces.
//
// Storage, Notifications, and Events are Phase 1 stubs — calling them returns
// ErrUnimplemented. Real impls land when a consumer needs them (Phase 3+).
type coreImpl struct {
	app *App
}

// newCore constructs a coreImpl bound to the given App. Safe to call after
// App.Startup has constructed the underlying APIs (mailAPI, contactsAPI, etc.).
func newCore(a *App) *coreImpl {
	return &coreImpl{app: a}
}

func (c *coreImpl) Mail() coreapi.Mail           { return c.app.mailAPI }
func (c *coreImpl) Composer() coreapi.Composer   { return c.app.composerAPI }
func (c *coreImpl) Contacts() coreapi.Contacts   { return c.app.contactsAPI }
func (c *coreImpl) Auth() coreapi.Auth           { return c.app.authBroker }
func (c *coreImpl) UI() coreapi.UI               { return c.app.uiRegistry }
func (c *coreImpl) Notifications() coreapi.Notifications { return stubNotifications{} }
func (c *coreImpl) Storage() coreapi.Storage     { return stubStorage{} }
func (c *coreImpl) Events() coreapi.EventBus     { return stubEventBus{} }

// Extension returns the typed handle published by another extension via its
// api.go, or (nil, false) if the extension is not loaded. Phase 2a doesn't
// yet expose cross-extension typed handles — a consumer needs to motivate
// the wiring. Returning (nil, false) is the correct "not found" response.
func (c *coreImpl) Extension(id string) (any, bool) {
	return nil, false
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

func (k stubKV) Get(key string) (string, error)         { return "", fmt.Errorf("storage.KV: extension %q has no host-provided KV in Phase 2a (use its own Store directly)", k.extensionID) }
func (k stubKV) Set(key, value string) error            { return coreapi.ErrUnimplemented }
func (k stubKV) Delete(key string) error                { return coreapi.ErrUnimplemented }
func (k stubKV) List(prefix string) ([]string, error)   { return nil, coreapi.ErrUnimplemented }

type stubEventBus struct{}

func (stubEventBus) Publish(name string, payload any) error {
	return coreapi.ErrUnimplemented
}

func (stubEventBus) Subscribe(name string, handler func(payload any)) (coreapi.Unsubscribe, error) {
	return nil, coreapi.ErrUnimplemented
}

// compile-time check: coreImpl satisfies coreapi.Core
var _ coreapi.Core = (*coreImpl)(nil)
