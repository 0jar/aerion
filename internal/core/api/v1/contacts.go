package v1

// ContactEventType identifies a kind of contact event extensions can subscribe to.
type ContactEventType string

const (
	ContactEventAdded   ContactEventType = "added"
	ContactEventUpdated ContactEventType = "updated"
	ContactEventDeleted ContactEventType = "deleted"
)

// ContactEvent is delivered to subscribers of Contacts.SubscribeToContactEvents.
type ContactEvent struct {
	Type      ContactEventType `json:"type"`
	ContactID string           `json:"contactId"`
}

// ContactPatch is the optional-fields shape passed to Contacts.UpdateContact.
// Pointer fields distinguish "leave unchanged" (nil) from "set to empty"
// (non-nil pointer to zero value). Phase 2b.1 only ships Name; richer fields
// (Emails, Phone, Address, Notes) get added when 2b.2/2b.3 wire the
// provider-side write paths that need them.
type ContactPatch struct {
	Name *string `json:"name,omitempty"`
}

// ContactCreateInput is the shape passed to Contacts.CreateContact.
//
// SourceID selects where the new contact lives:
//   - "" or "local" or "local:manual" → local manual contact (Aerion's
//     own SQLite store). Phase 2b.1 ships only this path.
//   - "local:collected"               → REJECTED. The Collected sub-source is
//     read-only by design: those entries are derived from sent-mail
//     recipients, not user-typed.
//   - <CardDAV source UUID>           → returns ErrUnimplemented in 2b.1;
//     filled in by 2b.2 with the WebDAV PUT path.
//   - Future provider routing         → returns ErrUnimplemented; filled in by
//     2b.3 (Google People / MS Graph).
//
// Future fields (Emails []string, Phone, Address, Notes) get added when 2b.2
// fills the CardDAV write path and the vCard builder reveals the shape.
type ContactCreateInput struct {
	SourceID string `json:"sourceId,omitempty"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
}

// Contacts is the read/write/subscribe surface for contacts.
//
// All methods scoped to data the extension manages — local contact store +
// per-source mirror tables. Write methods dispatch by source under the hood:
// local (sent-recipient) contacts mutate through the host's contact.Store;
// CardDAV / Google / Microsoft writes are scoped to their per-extension OAuth
// path (Phase 2b.2 / 2b.3) and return ErrUnimplemented until those land.
type Contacts interface {
	SearchContacts(query string, limit int) ([]Contact, error)
	GetContact(emailOrID string) (*Contact, error)
	ListContacts(filter ContactFilter) ([]Contact, error)
	CreateContact(input ContactCreateInput) (id string, err error)
	UpdateContact(id string, patch ContactPatch) error
	DeleteContact(id string) error
	SubscribeToContactEvents(types []ContactEventType) (ch <-chan ContactEvent, cancel Unsubscribe, err error)
}
