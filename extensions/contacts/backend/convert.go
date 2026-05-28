package backend

import (
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// fromLocal converts a core contact.Contact into the API-surface Contact.
//
// Core contacts are keyed by email, so we use the email itself as the ID. The
// Source field (e.g. "aerion", "google", "vcard", "carddav") becomes SourceID
// for search results where the user hasn't picked a specific source.
func fromLocal(c *contact.Contact) coreapi.Contact {
	updated := c.LastUsed
	if updated.IsZero() {
		updated = c.CreatedAt
	}
	return coreapi.Contact{
		ID:        c.Email,
		Name:      c.DisplayName,
		Emails:    []string{c.Email},
		SourceID:  c.Source,
		UpdatedAt: updated,
	}
}

// fromCardDAV converts a carddav.Contact into the API-surface Contact, scoped
// to a known sourceID. The caller knows which CardDAV source it queried, so
// we propagate that ID into the result rather than re-deriving it via a join.
func fromCardDAV(c *carddav.Contact, sourceID string) coreapi.Contact {
	return coreapi.Contact{
		ID:        c.ID,
		Name:      c.DisplayName,
		Emails:    []string{c.Email},
		SourceID:  sourceID,
		UpdatedAt: c.SyncedAt,
	}
}
