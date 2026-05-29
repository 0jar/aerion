package backend

import (
	"fmt"
	"strings"

	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// Source IDs for Aerion's core local contact store. CardDAV sources use their
// own UUIDs (one per configured source).
//
// The local source has two sub-categories distinguished by `contacts.kind`:
//   - manual    → entries the user added via the Add Contact UI
//   - collected → auto-collected from sent-mail recipients
//
// The "Local" parent (SourceIDLocal) returns both. Sub-source values select
// one kind only.
const (
	SourceIDLocal          = "local"
	SourceIDLocalManual    = "local:manual"
	SourceIDLocalCollected = "local:collected"
)

// isLocalSource reports whether the given filter sourceID targets the local
// store (either the parent "local" or a sub-source like "local:manual").
func isLocalSource(id string) bool {
	return id == SourceIDLocal || strings.HasPrefix(id, SourceIDLocal+":")
}

// localKindFromSourceID returns the `contacts.kind` filter value for a local
// sub-source ID, or "" for the parent "local" (= no filter, return both).
func localKindFromSourceID(id string) string {
	switch id {
	case SourceIDLocalManual:
		return "manual"
	case SourceIDLocalCollected:
		return "collected"
	default:
		return ""
	}
}

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
//
// For email-shaped IDs the lookup tries the local store first. When the local
// store misses (common case for the "All" view where a CardDAV contact's row
// surfaces with its email as the synthetic ID — the merged-search bridge
// drops the CardDAV UUID), the method falls back to the CardDAV store keyed
// by email. This keeps the detail pane populated for CardDAV-only contacts
// without requiring the merge bridge to round-trip the UUID through
// contact.Contact (which has no ID field).
func (a *API) GetContact(emailOrID string) (*coreapi.Contact, error) {
	if emailOrID == "" {
		return nil, nil
	}
	if a.localStore == nil {
		return nil, nil
	}

	// Try as record_id first. Local records have IDs like "local-<email>" and
	// CardDAV records have UUIDs — both work as record IDs. This handles the
	// majority case where the caller already has the canonical id.
	rec, err := a.localStore.GetRecord(emailOrID)
	if err != nil {
		return nil, fmt.Errorf("contacts.GetContact: %w", err)
	}
	if rec != nil {
		out := fromRecord(rec)
		return &out, nil
	}

	// Fall back to email lookup. Used when a caller passes a bare email
	// (e.g., from autocomplete results whose ID is the email rather than the
	// canonical record_id).
	if strings.Contains(emailOrID, "@") {
		rec, err := a.localStore.GetRecordByEmail(emailOrID)
		if err != nil {
			return nil, fmt.Errorf("contacts.GetContact: %w", err)
		}
		if rec != nil {
			out := fromRecord(rec)
			return &out, nil
		}
	}
	return nil, nil
}

// ListContacts returns contacts filtered by SourceID:
//   - ""                       → merged search across all sources (uses Query if set)
//   - SourceIDLocal            → all local contacts (manual + collected)
//   - SourceIDLocalManual      → user-added local contacts only
//   - SourceIDLocalCollected   → auto-collected local contacts only
//   - <carddav uuid>           → a specific CardDAV source, paged via offset/limit
func (a *API) ListContacts(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	switch {
	case filter.SourceID == "":
		return a.listMerged(filter)
	case isLocalSource(filter.SourceID):
		return a.listLocal(filter)
	default:
		return a.listCardDAV(filter)
	}
}

// CreateContact creates a new contact in the source identified by input.SourceID.
// Source dispatch:
//
//   - "", "local", "local:manual" → local manual entry via contact.Store.Create
//     (kind='manual', name_overridden=1, send_count=0). Returns the email as
//     the new contact's id. Errors with ErrContactExists when the email is
//     already present.
//   - "local:collected"            → REJECTED. The Collected sub-source is
//     read-only by design — those entries are auto-derived from sent-mail.
//   - CardDAV / Google / Microsoft → returns ErrUnimplemented. Filled in by
//     Phase 2b.2 (CardDAV PUT) and 2b.3 (Google People / MS Graph).
//
// Email is normalized (trim + lowercase) before storage; the returned id is
// the normalized email.
func (a *API) CreateContact(input coreapi.ContactCreateInput) (string, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return "", fmt.Errorf("contacts.CreateContact: email is required")
	}
	if !strings.Contains(email, "@") {
		return "", fmt.Errorf("contacts.CreateContact: email is not valid")
	}

	switch {
	case input.SourceID == "" || input.SourceID == SourceIDLocal || input.SourceID == SourceIDLocalManual:
		if a.localStore == nil {
			return "", fmt.Errorf("contacts.CreateContact: local store unavailable")
		}
		if err := a.localStore.Create(email, strings.TrimSpace(input.Name)); err != nil {
			return "", err
		}
		return email, nil
	case input.SourceID == SourceIDLocalCollected:
		return "", fmt.Errorf("contacts.CreateContact: cannot manually create a Collected contact (auto-derived from sent mail)")
	default:
		// CardDAV / OAuth-source contacts.
		return "", coreapi.ErrUnimplemented
	}
}

// UpdateContact mutates a contact by id. Source dispatch follows the same id
// heuristic as GetContact (email → local, UUID → CardDAV):
//
//   - Local (email): updates display_name via contact.Store.UpdateName, which
//     also sets name_overridden=1 so future AddOrUpdate calls on sent mail
//     won't clobber the user edit. Phase 2b.1 supports only the Name field;
//     other patch fields are ignored.
//   - CardDAV / Google / Microsoft (UUID): returns ErrUnimplemented. Filled
//     in by Phase 2b.2 (CardDAV PUT) and 2b.3 (Google People / MS Graph write
//     paths via the Auth Broker).
//
// Empty/nil patch (no fields set) is a no-op success — callers can issue a
// "touch" call without sending field updates.
func (a *API) UpdateContact(id string, patch coreapi.ContactPatch) error {
	if id == "" {
		return fmt.Errorf("contacts.UpdateContact: id is required")
	}
	if a.localStore == nil {
		return fmt.Errorf("contacts.UpdateContact: local store unavailable")
	}

	// Resolve the id to a record. Try as record_id first (works for both the
	// "local-<email>" form and CardDAV UUIDs); fall back to email lookup if
	// the caller passed a bare email.
	rec, err := a.localStore.GetRecord(id)
	if err != nil {
		return fmt.Errorf("contacts.UpdateContact: %w", err)
	}
	if rec == nil && strings.Contains(id, "@") {
		rec, err = a.localStore.GetRecordByEmail(id)
		if err != nil {
			return fmt.Errorf("contacts.UpdateContact: %w", err)
		}
	}
	if rec == nil {
		// No matching record. Idempotent miss.
		return nil
	}

	// Local-source records: edit via the record-level helper. CardDAV/OAuth
	// records still need a real write path — return ErrUnimplemented (2b.2.b).
	if rec.Source != "local" {
		return coreapi.ErrUnimplemented
	}
	if patch.Name == nil {
		// No fields set — successful no-op.
		return nil
	}
	return a.localStore.UpdateRecordName(rec.ID, *patch.Name)
}

// DeleteContact removes a contact by id. Source dispatch:
//
//   - Local (email): delegates to contact.Store.Delete.
//   - CardDAV / Google / Microsoft (UUID): returns ErrUnimplemented until
//     Phase 2b.2 / 2b.3 wires provider write paths.
//
// Idempotent on the local path (deleting a non-existent contact succeeds).
func (a *API) DeleteContact(id string) error {
	if id == "" {
		return fmt.Errorf("contacts.DeleteContact: id is required")
	}
	if a.localStore == nil {
		return fmt.Errorf("contacts.DeleteContact: local store unavailable")
	}

	// Resolve to a record. Try record_id first (works for both "local-<email>"
	// and CardDAV UUIDs); fall back to email lookup if the caller passed a bare
	// email.
	rec, err := a.localStore.GetRecord(id)
	if err != nil {
		return fmt.Errorf("contacts.DeleteContact: %w", err)
	}
	if rec == nil && strings.Contains(id, "@") {
		rec, err = a.localStore.GetRecordByEmail(id)
		if err != nil {
			return fmt.Errorf("contacts.DeleteContact: %w", err)
		}
	}
	if rec == nil {
		// Idempotent miss.
		return nil
	}

	// Local records: cascade-delete via the record id. CardDAV/OAuth need
	// server-side DELETE — 2b.2.b wires that up.
	if rec.Source != "local" {
		return coreapi.ErrUnimplemented
	}
	return a.localStore.DeleteRecord(rec.ID)
}

// SubscribeToContactEvents is scaffolded; Phase 3+ wires through a core
// event bus once one exists.
func (a *API) SubscribeToContactEvents(types []coreapi.ContactEventType) (<-chan coreapi.ContactEvent, coreapi.Unsubscribe, error) {
	return nil, func() {}, coreapi.ErrUnimplemented
}

// listLocal returns local contacts as one-row-per-record (Phase 2b.2.a),
// with multi-field sub-tables hydrated. Fixes the legacy duplicate-row UX
// wart (one row per email).
func (a *API) listLocal(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	if a.localStore == nil {
		return nil, nil
	}
	kind := localKindFromSourceID(filter.SourceID)
	records, err := a.localStore.ListRecords(contact.RecordFilter{
		Source: "local",
		Kind:   kind,
		Query:  filter.Query,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("contacts.ListContacts (local): %w", err)
	}
	out := make([]coreapi.Contact, 0, len(records))
	for _, rec := range records {
		out = append(out, fromRecord(rec))
	}
	return out, nil
}

// listCardDAV returns CardDAV contacts as one-row-per-record, scoped to a
// specific source. Uses ListRecordIDsForSource (which JOINs through addressbooks
// to the source) + per-id contact.Store.GetRecord to hydrate the full
// multi-field shape.
func (a *API) listCardDAV(filter coreapi.ContactFilter) ([]coreapi.Contact, error) {
	if a.carddavStore == nil || a.localStore == nil {
		return nil, nil
	}
	ids, err := a.carddavStore.ListRecordIDsForSource(filter.SourceID, filter.Query, filter.Offset, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("contacts.ListContacts (carddav %s): %w", filter.SourceID, err)
	}
	out := make([]coreapi.Contact, 0, len(ids))
	for _, id := range ids {
		rec, err := a.localStore.GetRecord(id)
		if err != nil {
			continue
		}
		if rec == nil {
			continue
		}
		c := fromRecord(rec)
		// Override SourceID with the actual sidebar source UUID the caller
		// asked for (the record's source_ref is the addressbook id, not the
		// source id — the join in ListRecordIDsForSource scoped to the source).
		c.SourceID = filter.SourceID
		out = append(out, c)
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
