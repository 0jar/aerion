package app

import (
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

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

// UpdateLocalContact renames a local (sent-recipient) contact. Routes through
// the Contacts extension's API (a.contactsAPI.UpdateContact) which dispatches
// by source under the hood — local id (contains @) → contact.Store.UpdateName
// (which also sets name_overridden=1). Wails-bound surface; gated on the
// extension being enabled.
//
// `email` is the contact's primary key (lower-cased). `name` is the new
// display name; empty string is allowed (clears the visible name but keeps
// the override flag, so auto-collection still won't reset it).
//
// The "Local" in the method name is historical — this method handles local
// edits today. When 2b.2/2b.3 land, a more general App.UpdateContact will
// dispatch by source via the same API and this one stays as a thin alias.
func (a *App) UpdateLocalContact(email, name string) error {
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
	return a.contactsAPI.UpdateContact(email, coreapi.ContactPatch{Name: &name})
}

// DeleteLocalContact removes a local contact entirely. Routes through the
// extension API for SDK consistency with reads + UpdateContact. Wails-bound;
// gated on the extension being enabled. Idempotent on the local path.
//
// Note: there's a separate top-level `App.DeleteContact` that's been around
// since pre-extension days for legacy callers. This one is gated to the
// extension's enabled state for the extension UI to call.
func (a *App) DeleteLocalContact(email string) error {
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
	return a.contactsAPI.DeleteContact(email)
}
