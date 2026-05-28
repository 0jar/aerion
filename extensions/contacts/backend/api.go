package backend

import (
	"fmt"
	"strings"

	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// SourceIDLocal is the synthetic SourceID for Aerion's core local contact
// store (sent-recipients + vCard files). CardDAV sources use their own UUIDs.
const SourceIDLocal = "local"

// API implements coreapi.Contacts by wrapping the existing core contact.Store
// and carddav.Store. Phase 2a is read-only; write methods will land in Phase
// 2b alongside the Auth Broker's first real consumer.
type API struct {
	localStore   *contact.Store
	carddavStore *carddav.Store
}

// NewAPI constructs the Contacts API wrapper. Either store may be nil — the
// wrapper degrades gracefully (e.g., a profile with no CardDAV sources has a
// nil carddavStore; search still returns local results).
func NewAPI(localStore *contact.Store, carddavStore *carddav.Store) *API {
	return &API{localStore: localStore, carddavStore: carddavStore}
}

// SearchContacts delegates to the core contact store's merged search across
// local, vCard, and CardDAV sources. The query is matched against email and
// display name; ranking is by send count + recency + source priority.
func (a *API) SearchContacts(query string, limit int) ([]coreapi.Contact, error) {
	if a.localStore == nil {
		return nil, nil
	}
	results, err := a.localStore.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("contacts.SearchContacts: %w", err)
	}
	out := make([]coreapi.Contact, 0, len(results))
	for _, c := range results {
		out = append(out, fromLocal(c))
	}
	return out, nil
}

// GetContact looks up a contact by email (if the argument contains '@') or by
// CardDAV UUID otherwise. Returns (nil, nil) when not found.
func (a *API) GetContact(emailOrID string) (*coreapi.Contact, error) {
	if emailOrID == "" {
		return nil, nil
	}
	if strings.Contains(emailOrID, "@") {
		if a.localStore == nil {
			return nil, nil
		}
		c, err := a.localStore.Get(emailOrID)
		if err != nil {
			return nil, fmt.Errorf("contacts.GetContact: %w", err)
		}
		if c == nil {
			return nil, nil
		}
		out := fromLocal(c)
		return &out, nil
	}

	if a.carddavStore == nil {
		return nil, nil
	}
	c, err := a.carddavStore.GetContactByID(emailOrID)
	if err != nil {
		return nil, fmt.Errorf("contacts.GetContact: %w", err)
	}
	if c == nil {
		return nil, nil
	}
	// SourceID unknown from this lookup alone — leave empty rather than
	// re-derive via a join. Phase 2c can add a richer detail-fetch path.
	out := fromCardDAV(c, "")
	return &out, nil
}

// ListContacts returns contacts filtered by SourceID:
//   - ""               → merged search across all sources (uses Query if set)
//   - SourceIDLocal    → core local contacts only
//   - <carddav uuid>   → a specific CardDAV source, paged via offset/limit
func (a *API) ListContacts(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	switch {
	case filter.SourceID == SourceIDLocal:
		return a.listLocal(filter)
	case filter.SourceID != "":
		return a.listCardDAV(filter)
	default:
		return a.listMerged(filter)
	}
}

// SubscribeToContactEvents is scaffolded; Phase 3+ wires through a core
// event bus once one exists.
func (a *API) SubscribeToContactEvents(types []coreapi.ContactEventType) (<-chan coreapi.ContactEvent, coreapi.Unsubscribe, error) {
	return nil, func() {}, coreapi.ErrUnimplemented
}

func (a *API) listLocal(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	if a.localStore == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	// When a query is set, route to Search (which already filters across
	// local contacts). Without a query, fall back to List for stable ordering.
	if filter.Query != "" {
		results, err := a.localStore.Search(filter.Query, filter.Offset+limit)
		if err != nil {
			return nil, fmt.Errorf("contacts.ListContacts (local search): %w", err)
		}
		if filter.Offset >= len(results) {
			return nil, nil
		}
		slice := results[filter.Offset:]
		out := make([]coreapi.Contact, 0, len(slice))
		for _, c := range slice {
			out = append(out, fromLocal(c))
		}
		return out, nil
	}
	// contact.Store.List doesn't support offset natively; for Phase 2a we
	// fetch (offset+limit) and slice. Local stores are small (sent-recipients
	// only), so this is acceptable until a paged variant is added.
	fetched, err := a.localStore.List(filter.Offset + limit)
	if err != nil {
		return nil, fmt.Errorf("contacts.ListContacts (local): %w", err)
	}
	if filter.Offset >= len(fetched) {
		return nil, nil
	}
	slice := fetched[filter.Offset:]
	out := make([]coreapi.Contact, 0, len(slice))
	for _, c := range slice {
		out = append(out, fromLocal(c))
	}
	return out, nil
}

func (a *API) listCardDAV(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	if a.carddavStore == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	contacts, err := a.carddavStore.ListContactsPaged(filter.SourceID, filter.Query, filter.Offset, limit)
	if err != nil {
		return nil, fmt.Errorf("contacts.ListContacts (carddav %s): %w", filter.SourceID, err)
	}
	out := make([]coreapi.Contact, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, fromCardDAV(c, filter.SourceID))
	}
	return out, nil
}

func (a *API) listMerged(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	if a.localStore == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	// "All" view: always merge local + vCard + CardDAV via contact.Store.Search.
	// Empty query → LIKE '%%' in each source's SQL = match all. The merge +
	// dedupe by email happens inside contact.Store.Search (which uses the
	// carddavSearchFn bridge wired in app.go Startup). Offset is unsupported
	// by Search; callers paginate by raising limit until "more" is needed.
	return a.SearchContacts(filter.Query, limit)
}
