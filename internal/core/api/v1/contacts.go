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

// Contacts is the read/subscribe surface for contacts.
//
// All methods scoped to data already in Aerion's core contact store. Extensions
// adding their own contact sources expose them through their own API surface.
type Contacts interface {
	SearchContacts(query string, limit int) ([]Contact, error)
	GetContact(emailOrID string) (*Contact, error)
	ListContacts(filter ContactFilter) ([]Contact, error)
	SubscribeToContactEvents(types []ContactEventType) (ch <-chan ContactEvent, cancel Unsubscribe, err error)
}
