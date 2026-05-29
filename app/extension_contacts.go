package app

import (
	"errors"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// emitContactsConflict translates a *coreapi.ErrConflict from a write path
// into a contacts:conflict event the extension's frontend listens for. The
// store has already refreshed the local cache from the source before this
// fires, so the frontend's response is "toast + reload" — no data carried
// in the payload beyond the contact id (the UI re-fetches the fresh row).
//
// Returns true when the error WAS a conflict (and an event was emitted);
// false otherwise so the caller can keep treating non-conflict errors as
// hard failures.
func (a *App) emitContactsConflict(err error) bool {
	var conflict *coreapi.ErrConflict
	if !errors.As(err, &conflict) {
		return false
	}
	wailsRuntime.EventsEmit(a.ctx, "contacts:conflict", map[string]string{
		"contactId": conflict.ContactID,
		"message":   conflict.Message,
	})
	return true
}

// ListContactsForBrowse is the Wails-bound entry for the Contacts extension's
// browse pane. Returns contacts filtered by sourceID:
//   - ""                            → merged (local + carddav, search applied)
//   - extcontacts.SourceIDLocal     → core local contacts only
//   - <carddav source UUID>         → contacts from a specific CardDAV source
//
// Guards on the contacts extension being enabled — disabled extensions return
// nil so the frontend can call this unconditionally without checking state.
func (a *App) ListContactsForBrowse(query, sourceID string, limit, offset int) ([]coreapi.Contact, error) {
	enabled, err := a.settingsStore.IsExtensionEnabled("contacts")
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	if a.contactsAPI == nil {
		return nil, nil
	}
	return a.contactsAPI.ListContacts(coreapi.ContactFilter{
		Query:    query,
		SourceID: sourceID,
		Limit:    limit,
		Offset:   offset,
	})
}

// GetContactDetail returns a single contact by email (if argument contains
// '@') or by CardDAV UUID otherwise. Phase 2a returns the same shape as a
// list-row entry — Phase 2c may add richer detail (phone, address, notes).
func (a *App) GetContactDetail(emailOrID string) (*coreapi.Contact, error) {
	enabled, err := a.settingsStore.IsExtensionEnabled("contacts")
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	if a.contactsAPI == nil {
		return nil, nil
	}
	return a.contactsAPI.GetContact(emailOrID)
}

// CreateLocalContact creates a new manual local contact via the Contacts
// extension's API. Used by the Add Contact UI. Returns the new contact's id
// (the normalized email) so the frontend can switch the sidebar to the
// "Contacts" sub-source and select the new row.
//
// Wails-bound; gated on the extension being enabled. The API's source
// dispatch handles validation + ErrContactExists mapping; this method is a
// thin pass-through.
func (a *App) CreateLocalContact(email, name string) (string, error) {
	enabled, err := a.settingsStore.IsExtensionEnabled("contacts")
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", nil
	}
	if a.contactsAPI == nil {
		return "", nil
	}
	return a.contactsAPI.CreateContact(coreapi.ContactCreateInput{
		SourceID: "local:manual",
		Email:    email,
		Name:     name,
	})
}

// UpdateLocalContact renames a contact via the Contacts extension's API,
// which dispatches by source under the hood:
//
//   - Local records → contact.Store.UpdateRecordName (which sets
//     name_overridden=1 so auto-collection won't clobber the edit).
//   - CardDAV records → PUT to the CardDAV server (gated on the source's
//     writable flag), then mirror server state locally. 412 conflicts surface
//     via the contacts:conflict event the UI listens for; on conflict the
//     method also returns nil (the user's edit was discarded but the local
//     cache now matches the server, so the UI can simply reload).
//
// `idOrEmail` is the record id (UUID for both local and CardDAV records) or
// — for back-compat — a bare email which the API falls back to. `name` is
// the new display name; empty string is allowed (clears the visible name
// but keeps name_overridden so auto-collection still won't reset it).
//
// The "Local" in the method name is historical — this method now handles
// CardDAV writes too. Wails-bound surface; gated on the extension being
// enabled.
func (a *App) UpdateLocalContact(idOrEmail, name string) error {
	enabled, err := a.settingsStore.IsExtensionEnabled("contacts")
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if a.contactsAPI == nil {
		return nil
	}
	err = a.contactsAPI.UpdateContact(idOrEmail, coreapi.ContactPatch{Name: &name})
	if a.emitContactsConflict(err) {
		// Local cache refreshed; UI reloads via the event. Don't bubble the
		// conflict as a write failure — the user's intent is acknowledged,
		// just superseded by the server.
		return nil
	}
	return err
}

// DeleteLocalContact removes a contact via the Contacts extension's API,
// which dispatches by source — local records cascade-delete in the unified
// store; CardDAV records DELETE on the server (gated on writable) and then
// cascade locally. 412 conflicts surface via the contacts:conflict event;
// on conflict the method returns nil (local cache has been refreshed).
//
// Idempotent on the local + 404 paths.
//
// Note: there's a separate top-level `App.DeleteContact` that's been around
// since pre-extension days for legacy callers. This one is gated to the
// extension's enabled state for the extension UI to call.
func (a *App) DeleteLocalContact(idOrEmail string) error {
	enabled, err := a.settingsStore.IsExtensionEnabled("contacts")
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if a.contactsAPI == nil {
		return nil
	}
	err = a.contactsAPI.DeleteContact(idOrEmail)
	if a.emitContactsConflict(err) {
		return nil
	}
	return err
}
