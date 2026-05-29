package backend

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	"github.com/hkdb/aerion/internal/credentials"
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
// and carddav.Store. credStore is needed to resolve per-source basic-auth
// passwords for CardDAV writes (Phase 2b.2.b.1); it may be nil in test
// fixtures that never exercise CardDAV writes.
type API struct {
	localStore   *contact.Store
	carddavStore *carddav.Store
	credStore    *credentials.Store
}

// NewAPI constructs the Contacts API wrapper. Any store may be nil — the
// wrapper degrades gracefully (a profile with no CardDAV sources has nil
// carddavStore; search still returns local results; CardDAV writes refuse with
// a clear error rather than panicking).
func NewAPI(localStore *contact.Store, carddavStore *carddav.Store, credStore *credentials.Store) *API {
	return &API{localStore: localStore, carddavStore: carddavStore, credStore: credStore}
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

// UpdateContact mutates a contact by id. Source dispatch:
//
//   - Local (record.Source == "local"): updates display_name via
//     contact.Store.UpdateRecordName, which also sets name_overridden=1 so
//     future AddOrUpdate calls on sent mail won't clobber the user edit.
//   - CardDAV (record.Source == "carddav"): PUTs the full record to the
//     CardDAV server gated on the source's writable flag, then mirrors the
//     server's accepted state locally. 412 conflicts surface as
//     *coreapi.ErrConflict after refreshing the local cache.
//   - Google / Microsoft (other source values): returns ErrUnimplemented;
//     filled in by Phase 2b.3 via the Auth Broker.
//
// Phase 2b.2.b.1 ships only the Name field in ContactPatch; other patch
// fields land in 2b.2.b.2 alongside the multi-field Edit dialog. The CardDAV
// write path is full-fidelity already (UpdateRecord serializes the entire
// record's current state), so 2b.2.b.2's UI is purely additive frontend work.
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

	// Local-source records: edit via the record-level helper.
	if rec.Source == "local" {
		if patch.Name == nil {
			return nil
		}
		return a.localStore.UpdateRecordName(rec.ID, *patch.Name)
	}

	// CardDAV: server-side PUT path. Reuses the source's basic-auth creds.
	if rec.Source == "carddav" {
		if patch.Name == nil {
			return nil
		}
		rec.Fn = strings.TrimSpace(*patch.Name)
		return a.writeCardDAVRecord(rec)
	}
	// Google/Microsoft (other source values) — write path lands in 2b.3.
	return coreapi.ErrUnimplemented
}

// DeleteContact removes a contact by id. Source dispatch:
//
//   - Local: cascade-deletes the record (and its sub-tables) via
//     contact.Store.DeleteRecord.
//   - CardDAV: DELETEs the resource from the server (gated on the source's
//     writable flag), then cascade-deletes locally. 412 conflicts surface as
//     *coreapi.ErrConflict after refreshing the local cache.
//   - Google / Microsoft: returns ErrUnimplemented until Phase 2b.3.
//
// Idempotent on the local + 404 paths (deleting a non-existent contact
// succeeds).
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

	// Local records: cascade-delete via the record id.
	if rec.Source == "local" {
		return a.localStore.DeleteRecord(rec.ID)
	}

	// CardDAV: server-side DELETE then cascade-delete locally.
	if rec.Source == "carddav" {
		return a.deleteCardDAVRecord(rec)
	}
	// Google/Microsoft — 2b.3.
	return coreapi.ErrUnimplemented
}

// writeCardDAVRecord is the shared CardDAV-write dispatch used by UpdateContact.
// Resolves the source for the record's addressbook, checks the writable flag,
// builds an authenticated client with the source's basic-auth creds, then
// delegates the PUT + local sync to carddav.Store.UpdateRecord.
//
// On a 412 conflict, refreshes the local cache from the server and returns a
// *coreapi.ErrConflict the Wails layer translates into a contacts:conflict
// event.
func (a *API) writeCardDAVRecord(rec *contact.Record) error {
	client, sourceID, err := a.cardDAVClientForRecord(rec)
	if err != nil {
		return err
	}
	if err := a.carddavStore.UpdateRecord(rec, client); err != nil {
		var pre *carddav.ErrPreconditionFailed
		if errors.As(err, &pre) {
			_ = a.carddavStore.RefreshRecordFromServer(rec.ID, client)
			return &coreapi.ErrConflict{ContactID: rec.ID, Message: "the contact was modified on the server"}
		}
		return fmt.Errorf("contacts.UpdateContact (carddav source %s): %w", sourceID, err)
	}
	return nil
}

// deleteCardDAVRecord is the shared CardDAV-delete dispatch. Same recipe as
// writeCardDAVRecord with DELETE in place of PUT.
func (a *API) deleteCardDAVRecord(rec *contact.Record) error {
	client, sourceID, err := a.cardDAVClientForRecord(rec)
	if err != nil {
		return err
	}
	if err := a.carddavStore.DeleteRecord(rec.ID, client); err != nil {
		var pre *carddav.ErrPreconditionFailed
		if errors.As(err, &pre) {
			_ = a.carddavStore.RefreshRecordFromServer(rec.ID, client)
			return &coreapi.ErrConflict{ContactID: rec.ID, Message: "the contact was modified on the server"}
		}
		return fmt.Errorf("contacts.DeleteContact (carddav source %s): %w", sourceID, err)
	}
	return nil
}

// cardDAVClientForRecord resolves the source for a CardDAV record (via the
// record's source_ref = addressbook_id), gates on the source's writable flag,
// fetches the basic-auth password from the credentials store, and returns a
// ready-to-use Client. Also returns the source.ID for error context.
func (a *API) cardDAVClientForRecord(rec *contact.Record) (*carddav.Client, string, error) {
	if a.carddavStore == nil {
		return nil, "", fmt.Errorf("carddav store unavailable")
	}
	source, err := a.carddavStore.GetSourceForAddressbook(rec.SourceRef)
	if err != nil {
		return nil, "", fmt.Errorf("lookup source for addressbook %s: %w", rec.SourceRef, err)
	}
	if source == nil {
		return nil, "", fmt.Errorf("no source owns addressbook %s", rec.SourceRef)
	}
	// Writability is a user-facing permission gate — fire it before the
	// credentials-store check so non-writable sources surface the right
	// error regardless of whether credentials are loadable.
	if !source.Writable {
		return nil, source.ID, fmt.Errorf("this source is not writable; enable write access in its settings")
	}
	if a.credStore == nil {
		return nil, source.ID, fmt.Errorf("credentials store unavailable")
	}
	password, err := a.credStore.GetCardDAVPassword(source.ID)
	if err != nil {
		return nil, source.ID, fmt.Errorf("get password for source %s: %w", source.ID, err)
	}
	client, err := carddav.NewClient(source.URL, source.Username, password)
	if err != nil {
		return nil, source.ID, fmt.Errorf("build carddav client: %w", err)
	}
	return client, source.ID, nil
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
