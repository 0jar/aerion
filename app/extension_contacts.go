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
