# Aerion Extension System

Developer reference for building first-party extensions on top of Aerion's core.

> **Status (v0.3.x):** First-party extensions only — extensions ship compiled into the binary and are individually toggleable in Settings. A community-extension runtime (dynamic loading, sandboxing, manifest verification) is deferred to v0.4+; see [§ Not yet implemented](#not-yet-implemented).

This doc is the contract every Aerion extension uses to interact with the host and with other extensions. Every claim is backed by a file path you can read directly — no second source of truth.

---

## Contents

1. [Overview](#overview)
2. [Architecture at a glance](#architecture-at-a-glance)
3. [Manifest + lifecycle](#manifest--lifecycle)
4. [`coreapi` reference](#coreapi-reference)
5. [Per-extension storage](#per-extension-storage)
6. [Auth Broker](#auth-broker)
7. [OAuth client configurations](#oauth-client-configurations)
8. [UI registration](#ui-registration)
9. [Account-setup hook contract](#account-setup-hook-contract)
10. [Lifecycle](#lifecycle)
11. [Settings keys](#settings-keys)
12. [Wails-bound surface](#wails-bound-surface)
13. [Testing conventions](#testing-conventions)
14. [Frontend conventions](#frontend-conventions)
15. [Extension UI Kit](#extension-ui-kit)
16. [Distribution model](#distribution-model)
17. [Not yet implemented](#not-yet-implemented)

---

## Overview

Aerion's extension system lets first-party extensions (Calendar, Contacts, Notes/Chat in the future) ship inside the same binary as Mail, while staying invisible to users who don't enable them. Design principles, in order of importance:

1. **Built-in, disabled by default.** Extensions compile into the binary but do nothing until enabled. Minimalists never see them.
2. **Per-extension SQLite isolation.** Each extension owns its own database file under `<dataDir>/extensions/<name>/data.db`. Extensions never query each other's tables — cross-extension data access goes through Go interfaces in `internal/core/api/v1`.
3. **Shared infrastructure stays shared.** One Wails process, one OAuth manager, one credential store, one IPC bus, one notification system. The extension system adds an additional **Auth Broker** layer so extensions never see access tokens or refresh tokens.
4. **Inline + detach pattern.** Every extension works inside the main window. Workflows can optionally pop out to a separate window via IPC (identical to the existing detached composer; not yet exercised by any extension in v0.3.x).
5. **Zero overhead when disabled.** An extension's DB file exists (migrations applied) but no sync, no background work, no UI rendering. The only cost is binary size.

Full architectural rationale lives in [`context/EXTENSION_ARCHITECTURE.md`](../context/EXTENSION_ARCHITECTURE.md). This doc is the **developer reference**; that doc is the **design rationale**.

---

## Architecture at a glance

```
┌─────────────────────────────────────────────────────────────────────┐
│  Aerion process (single binary, single WebKit view)                 │
│                                                                     │
│  ┌────────────────────────┐    ┌──────────────────────────────┐    │
│  │  Core (always running) │    │  Extensions (toggleable)     │    │
│  │                        │    │                              │    │
│  │  internal/account/     │    │  extensions/                 │    │
│  │  internal/folder/      │    │   contacts/                  │    │
│  │  internal/message/     │    │     manifest.json            │    │
│  │  internal/draft/       │    │     manifest.go              │    │
│  │  internal/contact/     │    │     backend/                 │    │
│  │  internal/carddav/     │    │       register.go, api.go..  │    │
│  │  internal/imap/        │    │     frontend/                │    │
│  │  internal/smtp/        │    │       components/, stores/   │    │
│  │  internal/oauth2/      │    │   (future: calendar/)        │    │
│  │  internal/credentials/ │    │                              │    │
│  │  internal/settings/    │    │  internal/extensions/        │    │
│  │  ...                   │    │   ui/, auth/, mail/, ...     │    │
│  │                        │    │   (host scaffolding)         │    │
│  └──────────┬─────────────┘    └──────────┬───────────────────┘    │
│             │                              │                        │
│             ▼                              ▼                        │
│         ┌───────────────────────────────────────────┐               │
│         │  internal/core/api/v1 — the contract      │               │
│         │  (Mail, Composer, Contacts, Auth, UI,     │               │
│         │   Storage, Notifications, EventBus, Core, │               │
│         │   Manifest, Extension)                    │               │
│         └───────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────┘
```

**The rule**: `extensions/<name>/` for things users toggle on/off. `internal/extensions/` for host scaffolding that always runs (UI registry, Auth Broker, Mail/Composer wrappers, per-extension Store wiring).

**Where to put each piece of code:**

| New code is... | Goes in |
|---|---|
| Extension's Go backend (logic, API impl, register hook) | `extensions/<name>/backend/` |
| Extension's manifest metadata | `extensions/<name>/manifest.json` + `manifest.go` (root, embeds JSON) |
| Extension's Svelte components | `extensions/<name>/frontend/components/` |
| Extension's Svelte stores | `extensions/<name>/frontend/stores/` |
| Extension's account-setup hook panel | `extensions/<name>/frontend/hooks/` |
| Extension's Wails-bound surface (App methods) | `app/extension_<name>.go` — see below |
| A type or interface ALL extensions might consume | `internal/core/api/v1/` |
| Shared host-side scaffolding (registry, broker, wrappers) | `internal/extensions/` |
| Host-owned UI used by the rail/dialog (not extension-specific) | `frontend/src/lib/components/rail/`, etc. |

**The `app/` exception:** Wails v2 binds methods on the `App` struct's receiver. Go doesn't allow methods on a type from another package. So `app/extension_<name>.go` stays in `app/` even though it conceptually belongs to the extension. The files are thin adapters that delegate to the extension's `backend/` package. When adding a new extension, add its Wails surface here in a new `extension_<name>.go` file.

Extensions DO NOT import from other extensions' Go packages. They go through `coreapi.Core.Extension(id)` (see [§ Core interface](#core-interface)).

---

## Manifest + lifecycle

Every first-party extension carries a `manifest.json` at its repo root and exposes a single Go object implementing `coreapi.Extension`. The host reads the manifest to build the Settings UI listing and calls `Register()` at startup to wire the extension's UI surfaces.

This shape is **subprocess-ready**: when community extensions land in v0.4+ (see [§ Distribution model](#distribution-model)), the same manifest fields and the same Register handshake will move across the IPC boundary unchanged. Nothing in the manifest references Go-specific concepts (no module paths, no compiled-type names).

### Manifest schema

[`extensions/contacts/manifest.json`](../extensions/contacts/manifest.json) for the canonical example:

```json
{
  "id": "contacts",
  "name": "Contacts",
  "version": "0.1.0",
  "description": "Browse contacts from your accounts (CardDAV, Google, Microsoft). Two-way edit/write capability lands in a future release.",
  "author": "Aerion",
  "minAerionVersion": "0.3.0",
  "capabilities": [
    "contacts.read",
    "ui.rail-tab",
    "ui.account-setup-hook"
  ]
}
```

| Field | Purpose |
|---|---|
| `id` | Canonical extension id. Must match the key used by `settings.AllExtensionKeys` and Settings flag (`extension_<id>_enabled`). |
| `name` | User-facing display name (Settings UI, rail-tab tooltip). |
| `version` | Semver. Surfaced in Settings → Extensions. |
| `description` | 1–2 sentence summary shown in the Settings listing. |
| `author` | Display name only. No URL. |
| `minAerionVersion` | Semver. Future host versions will refuse to load an extension whose minAerionVersion is higher than the running build. |
| `capabilities` | Coarse capability strings the extension declares. See [coreapi.Capability](../internal/core/api/v1/manifest.go) for the known set (e.g., `contacts.read`, `ui.rail-tab`). Unknown strings are treated as opaque so the set can grow without breaking older hosts. |

### Loading the manifest into Go

Place `manifest.json` at the extension root and a tiny `manifest.go` next to it that embeds the JSON:

```go
// extensions/contacts/manifest.go
package contacts

import (
    _ "embed"
    "encoding/json"
    coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

//go:embed manifest.json
var manifestJSON []byte

func Manifest() coreapi.Manifest {
    var m coreapi.Manifest
    if err := json.Unmarshal(manifestJSON, &m); err != nil {
        panic("contacts: manifest.json is malformed (build-time bug): " + err.Error())
    }
    return m
}
```

Why a separate root-level package for the manifest: Go's `//go:embed` directive can't traverse upward (no `../manifest.json`). Keeping the embed in the root-level package lets the backend implementation in `backend/` import the manifest data cleanly while keeping the manifest file semantically at the extension root.

### Extension lifecycle

[`internal/core/api/v1/manifest.go`](../internal/core/api/v1/manifest.go):

```go
type Extension interface {
    Manifest() Manifest
    Register(core Core) (Unregister, error)
}
```

**`Register` is called once per process at startup, regardless of whether the extension is currently enabled.** This matches the architecture-doc rule that descriptive UI registrations (rail tab, account-setup hook) persist across enable/disable cycles. Active behaviors (sync schedulers, background work) are gated separately by `IsExtensionEnabled` checks; they are NOT skipped at Register time.

The returned `Unregister` removes everything Register wired. Called by the host on process shutdown.

### Example: Contacts extension

[`extensions/contacts/backend/register.go`](../extensions/contacts/backend/register.go):

```go
type Extension struct {
    api      *API
    store    *Store
    manifest coreapi.Manifest
}

func New(localStore *contact.Store, carddavStore *carddav.Store, store *Store) *Extension {
    return &Extension{
        api:      NewAPI(localStore, carddavStore),
        store:    store,
        manifest: contacts.Manifest(),
    }
}

func (e *Extension) Manifest() coreapi.Manifest { return e.manifest }
func (e *Extension) API() *API { return e.api }

func (e *Extension) Register(core coreapi.Core) (coreapi.Unregister, error) {
    unregRail, err := core.UI().RegisterRailTab(coreapi.RailTabRequest{
        ExtensionID: e.manifest.ID,
        Label:       e.manifest.Name,
        Icon:        "mdi:account-multiple",
        Component:   "ContactsPane",
        Order:       10,
    })
    if err != nil { return nil, err }

    unregHook, err := core.UI().RegisterAccountSetupHook(coreapi.AccountSetupHookRequest{...})
    if err != nil { unregRail(); return nil, err }

    return func() { unregHook(); unregRail() }, nil
}
```

### Host-side startup

App.Startup iterates `a.knownExtensions []coreapi.Extension` and calls `Register` on each:

```go
a.contactsExt = extcontactsbe.New(a.contactStore, a.carddavStore, a.extContactsStore)
a.knownExtensions = []coreapi.Extension{a.contactsExt}

core := newCore(a)
for _, ext := range a.knownExtensions {
    unreg, err := ext.Register(core)
    if err != nil {
        log.Warn().Err(err).Str("extension", ext.Manifest().ID).Msg("Failed to register extension")
        continue
    }
    a.extensionUnregs = append(a.extensionUnregs, unreg)
}
```

When you add a new first-party extension, build its `Extension` struct and append to `a.knownExtensions`. Everything else (Settings UI listing, rail rendering, hook discovery) reads through this slice.

---

## `coreapi` reference

Package: `github.com/hkdb/aerion/internal/core/api/v1` (12 files; entirely interface + type declarations, no logic).

The full surface is defined in [`internal/core/api/v1/`](../internal/core/api/v1). Every extension method receives a `coreapi.Core` (see [§ Core interface](#core-interface)) at initialization. From there it grabs the surfaces it needs.

### Stability promise

`v1` is the stable API for Aerion v0.3.0+. **Non-breaking additions** (new methods on existing interfaces with sensible defaults, new event types, new fields on request structs with zero values) may land between minor releases. **Breaking changes** require introducing `v2` and keeping `v1` as a compatibility shim. For solo-dev scale this stays at `v1` indefinitely.

### `Core` interface

[`internal/core/api/v1/core.go`](../internal/core/api/v1/core.go)

```go
type Core interface {
    Mail() Mail
    Composer() Composer
    Contacts() Contacts
    Auth() Auth
    Notifications() Notifications
    UI() UI
    Storage() Storage
    Events() EventBus

    // Extension returns the typed handle published by another extension via
    // its api.go interface, or (nil, false) if the extension is not enabled
    // or has not published a typed API.
    Extension(id string) (any, bool)
}
```

Extensions call `core.Mail().ListMessages(...)`, `core.Auth().HTTPClient(...)`, etc. For Phase 1 first-party extensions, all capabilities are implicitly granted; surfaces an extension isn't using simply aren't called.

### `Mail`

[`internal/core/api/v1/mail.go`](../internal/core/api/v1/mail.go)

```go
type Mail interface {
    // Read
    ListMessages(filter MessageFilter) ([]Message, error)
    GetMessage(id string, includeBody bool) (*Message, error)
    ListFolders(accountID string) ([]Folder, error)
    GetSpecialFolder(accountID string, kind FolderKind) (*Folder, error)

    // Mutate — Phase 1 returns ErrUnimplemented; Phase 2+ wires through
    // app/actions.go so undo/sync/events fire identically to user actions.
    MoveMessage(id, destFolderID string) error
    Archive(id string) error
    Trash(id string) error
    SetFlags(id string, flags Flags) error
    AppendMessage(accountID, folderID string, raw []byte, flags Flags) error

    // Events
    SubscribeToMailEvents(types []MailEventType) (<-chan MailEvent, Unsubscribe, error)
}
```

Concrete impl: [`internal/extensions/mail/api.go`](../internal/extensions/mail/api.go). Read methods are wired. Mutators + `SubscribeToMailEvents` return `coreapi.ErrUnimplemented` in v0.3.0.

### `Composer`

[`internal/core/api/v1/compose.go`](../internal/core/api/v1/compose.go)

```go
type Composer interface {
    OpenComposer(req ComposeRequest) error
}
```

Phase 1 impl ([`internal/extensions/compose/api.go`](../internal/extensions/compose/api.go)) builds an RFC 6068 mailto URL from the request and delegates to the host's existing `OpenComposerWindow`. Attachments and `ReplyTo` are deferred (`ErrUnimplemented`) because they need composer-state integration beyond mailto.

### `Contacts`

[`internal/core/api/v1/contacts.go`](../internal/core/api/v1/contacts.go)

```go
type Contacts interface {
    SearchContacts(query string, limit int) ([]Contact, error)
    GetContact(emailOrID string) (*Contact, error)
    ListContacts(filter ContactFilter) ([]Contact, error)
    SubscribeToContactEvents(types []ContactEventType) (<-chan ContactEvent, Unsubscribe, error)
}
```

Concrete impl: [`internal/extensions/contacts/api.go`](../internal/extensions/contacts/api.go) (Phase 2a). Search/Get/List wrap the existing core `contact.Store` + `carddav.Store`. `SubscribeToContactEvents` returns `ErrUnimplemented` until a core event bus exists.

**`ContactFilter.SourceID` conventions:**

| Value | Behavior |
|---|---|
| `""` (empty) | Merged listing — when `Query` is set, calls `contact.Store.Search` (local + vCard + CardDAV merged + ranked). When `Query` is empty, falls back to local-only list. |
| `"local"` (`extcontacts.SourceIDLocal`) | Aerion's core local contacts only (sent recipients, vCard). Paged via `Limit`/`Offset`. |
| `<carddav source UUID>` | Contacts from a specific CardDAV source. Uses `carddav.Store.ListContactsPaged`; only enabled sources + addressbooks are returned. |

**`GetContact` argument:** if the string contains `@`, treated as an email and looked up in the core local store; otherwise treated as a CardDAV UUID via `carddav.Store.GetContactByID`. Returns `(nil, nil)` when not found — never an error for missing.

### `Auth`

[`internal/core/api/v1/auth.go`](../internal/core/api/v1/auth.go)

```go
type Auth interface {
    HTTPClient(accountID string, scopes []AuthScope) (*http.Client, error)
    IMAPClient(accountID string, requiredCaps []string) (IMAPClient, error)
    SMTPClient(accountID string) (SMTPClient, error)
}
```

Extensions get pre-configured HTTP clients with bearer token injection and transparent refresh-on-401. They never see access tokens, refresh tokens, or passwords. Full details in [§ Auth Broker](#auth-broker).

### `Notifications`

[`internal/core/api/v1/notifications.go`](../internal/core/api/v1/notifications.go)

```go
type Notifications interface {
    Show(req NotifyRequest) error
}
```

Phase 1: interface only. Phase 3+ wires to the existing `internal/notification` package. `NotifyClickAction` supports `open-extension`, `open-deep-link`, and `custom` handlers.

### `UI`

[`internal/core/api/v1/ui.go`](../internal/core/api/v1/ui.go)

```go
type UI interface {
    RegisterRailTab(req RailTabRequest) (Unregister, error)
    RegisterSettingsTab(req SettingsTabRequest) (Unregister, error)
    RegisterContextMenuItem(req ContextMenuRequest) (Unregister, error)
    RegisterInboxView(req InboxViewRequest) (Unregister, error)
    RegisterAccountSetupHook(req AccountSetupHookRequest) (Unregister, error)
}
```

Concrete impl: [`internal/extensions/ui/registry.go`](../internal/extensions/ui/registry.go) (Phase 2a). All five registration methods are wired and concurrency-safe (`RWMutex`-protected map per kind). `RailTab` and `AccountSetupHook` have real frontend consumers in v0.3.x; the other three (`SettingsTab`, `ContextMenuItem`, `InboxView`) accept registrations but no consumer reads them yet. See [§ UI registration](#ui-registration).

### `Storage`

[`internal/core/api/v1/storage.go`](../internal/core/api/v1/storage.go)

```go
type Storage interface {
    KV(extensionID string) KVStore
}

type KVStore interface {
    Get(key string) (string, error)
    Set(key, value string) error
    Delete(key string) error
    List(prefix string) ([]string, error)
}
```

For small config (per-extension preferences, sync tokens, etc.) that doesn't warrant SQL tables. Per-extension SQLite is implicit: each extension's `store.go` opens its own DB. See [§ Per-extension storage](#per-extension-storage).

### `EventBus`

[`internal/core/api/v1/events.go`](../internal/core/api/v1/events.go)

```go
type EventBus interface {
    Publish(name string, payload any) error
    Subscribe(name string, handler func(payload any)) (Unsubscribe, error)
}
```

Phase 1: interface only. Phase 3+ ships a concrete impl for cross-extension loose coupling.

### Shared types

[`internal/core/api/v1/types.go`](../internal/core/api/v1/types.go)

- `Address{ Name, Email }`
- `Attachment{ Filename, MIMEType, Size, Data, Path, IsInline, ContentID }`
- `MessageRef{ AccountID, FolderID, MessageID }` — Aerion DB id, not RFC 5322 Message-ID
- `Flags{ Seen, Flagged, Answered, Draft, Deleted, Forwarded }`
- `FolderKind` — `inbox|sent|drafts|trash|archive|spam|all|starred`
- `Message`, `Folder` — API-surface mirrors of internal storage types (decoupled so internal storage can evolve)
- `MessageFilter`, `ContactFilter`
- `Contact{ ID, Name, Emails, SourceID, UpdatedAt }`
- `Unregister`, `Unsubscribe` — `func()` aliases returned by registration / subscription methods

### Sentinel errors

[`internal/core/api/v1/errors.go`](../internal/core/api/v1/errors.go)

| Error | When |
|---|---|
| `ErrDisabled` | Extension or feature is disabled; treat as a benign "feature off" signal |
| `ErrCapabilityDenied` | Method called on a capability not granted (Phase 1: never happens for first-party — all-or-nothing) |
| `ErrAccountNotFound` | API call references an account that doesn't exist |
| `ErrUnimplemented` | Method scaffolded but not implemented in this release |
| `*ErrAdditionalConsentRequired{ AccountID, ClientConfigID, MissingScopes }` | Auth Broker needs additional OAuth scopes; host handles consent, extension retries |

Use `errors.Is(err, coreapi.ErrXxx)` for sentinel matching. `ErrAdditionalConsentRequired` is a typed error (not a sentinel) — type-assert to read `MissingScopes`.

---

## Per-extension storage

Package: [`internal/extensions/`](../internal/extensions/) (files [`store.go`](../internal/extensions/store.go) and [`kv.go`](../internal/extensions/kv.go)).

```go
import "github.com/hkdb/aerion/internal/extensions"

func NewStore(dataDir string) (*extensions.Store, error) {
    return extensions.OpenStore(dataDir, "myextension", []extensions.Migration{
        {Version: 1, SQL: `CREATE TABLE myitems (...)`},
        {Version: 2, SQL: `CREATE INDEX ...`},
    })
}
```

**What `OpenStore` does:**

1. Resolves the path to `<dataDir>/extensions/<name>/data.db` (creates parent dirs with 0700 perms)
2. Opens the DB via the standard `database.Open` (inherits WAL, busy timeout, 0600 file perms, etc.)
3. Creates the canonical `ext_kv` table BEFORE user migrations run, so KV is always available even with zero user tables
4. Creates an extension-private `migrations` table and applies user migrations in version order, idempotently

**Reaching the SQL:**

```go
db := store.DB()           // *sql.DB; for the extension's own tables only
store.Path()               // on-disk file path
kv := store.KV()           // coreapi.KVStore backed by ext_kv table
```

**Migrations** start at version 1, increment monotonically. Each runs inside a transaction. Already-applied versions are skipped on every startup. Each extension's migration sequence is INDEPENDENT — no global migration namespace.

**File location:** Linux `~/.local/share/aerion/extensions/<name>/data.db`, macOS `~/Library/Application Support/Aerion/extensions/<name>/data.db`, Windows `%LOCALAPPDATA%\aerion\extensions\<name>\data.db`. The `extensions/` parent is created by [`internal/platform/paths.go EnsureDirectories`](../internal/platform/paths.go).

**Lifecycle:** Per the architecture doc, stores open EAGERLY at `App.Startup`, regardless of whether the extension is currently enabled. This keeps schemas valid across enable/disable cycles — users can disable, the migrations stay applied, re-enabling is instantaneous.

**KV namespace:** [`internal/extensions/kv.go`](../internal/extensions/kv.go) implements `coreapi.KVStore` backed by the `ext_kv` table. Use it for sync tokens, view prefs, anything that doesn't warrant its own table. `Get` returns `("", nil)` for missing keys (no error). `Delete` is idempotent. `List(prefix)` returns sorted keys; `prefix=""` returns all.

---

## Auth Broker

Package: [`internal/extensions/auth/`](../internal/extensions/auth/) (files [`broker.go`](../internal/extensions/auth/broker.go), [`transport.go`](../internal/extensions/auth/transport.go), [`scope.go`](../internal/extensions/auth/scope.go)).

The Auth Broker is the ONLY way an extension reaches external services. Extensions never see access tokens, refresh tokens, or passwords. Token refresh is transparent. Multi-client-config routing handles the "Mail uses project A, Calendar/Contacts use project B" reality without forcing users to re-authenticate the unrelated service.

### `HTTPClient`

```go
client, err := core.Auth().HTTPClient(accountID, []coreapi.AuthScope{
    {Resource: "https://www.googleapis.com/auth/calendar.readonly",
     Reason:   "Read your calendar to sync events"},
})
if err != nil {
    var needConsent *coreapi.ErrAdditionalConsentRequired
    if errors.As(err, &needConsent) {
        // Don't try to fix this — the HOST is responsible for triggering
        // consent. Return ErrAdditionalConsentRequired up the call chain;
        // the host's Wails layer will surface the consent UI and the user
        // retries the action.
        return err
    }
    return fmt.Errorf("auth broker: %w", err)
}

// Use the client normally — bearer token + refresh-on-401 are transparent.
resp, err := client.Get("https://www.googleapis.com/calendar/v3/users/me/calendarList")
```

### Routing logic

When you call `HTTPClient(accountID, scopes)`:

1. Broker reads the account's existing Mail tokens to discover its provider (`google`, `microsoft`)
2. Broker picks a `ClientConfigID` for the requested scopes via [`internal/extensions/auth/scope.go resolveClientConfigID`](../internal/extensions/auth/scope.go):
   - If the extensions-OAuth client config is provisioned (`google-extensions` / `microsoft-extensions`), route there
   - Otherwise fall back to the mail config (graceful local-dev mode when the second OAuth project isn't set up yet)
3. Broker checks whether the account has tokens under that ClientConfigID covering the requested scopes
4. **Covered**: returns `*http.Client` whose Transport injects bearer + refreshes on 401
5. **Not covered**: returns `*coreapi.ErrAdditionalConsentRequired{ ... }` with the missing scopes

### Token refresh

[`internal/extensions/auth/transport.go bearerRefreshTransport`](../internal/extensions/auth/transport.go) handles refresh. It serializes refreshes per `(accountID, clientConfigID)` so N concurrent expired-token requests cause exactly one refresh.

### IMAP / SMTP

```go
imapClient, err := core.Auth().IMAPClient(accountID, []string{"SIEVE"})
smtpClient, err := core.Auth().SMTPClient(accountID)
```

Phase 1: both return `coreapi.ErrUnimplemented`. Phase 2+ wires them when a real consumer needs IMAP-via-broker (Sieve script management, custom X-* commands) or SMTP-via-broker (delayed-send queues).

---

## OAuth client configurations

Package: [`internal/oauth2/clientconfig.go`](../internal/oauth2/clientconfig.go).

Each first-party extension owns its own OAuth client (Google Cloud project / Azure AD registration). This is INTENTIONAL: it sets the precedent for future community extensions (no first-party shortcut to grandfather in), avoids re-verification cascade when Mail's project doesn't need to change, and the UX cost (one Google consent click per account + per extension) is acceptable because the browser is already signed in.

### Registry

```go
type ClientCredentials struct {
    ClientID     string
    ClientSecret string
}

// Known ids:
//   "google-mail"          — current verified Mail-scoped Google project
//   "google-extensions"    — extension-scoped Google project (Calendar/Contacts/...)
//   "microsoft-mail"       — current Mail-scoped Azure AD registration
//   "microsoft-extensions" — extension-scoped Azure AD registration
func ClientConfigForID(id string) (ClientCredentials, bool)
```

`ClientConfigForID` returns `(zero, false)` for configs that aren't yet provisioned (e.g., `"google-extensions"` before the second Google project is set up in the `aerion-creds` shim). The Auth Broker treats this as "fall back to the mail config" so local development works without provisioning a second project.

### Provider lookup

```go
provider, err := oauth2.GetProviderForClientConfig("google-extensions")
// provider.ClientID, provider.ClientSecret are populated from the extension's project
// provider.Scopes are the default Google scopes (override per-extension as needed)
```

### Provisioning a new client config

When you ship a real extension that needs its own OAuth project:

1. Create a new Google Cloud project (or Azure AD app registration) with the scopes your extension needs
2. Add the client_id/secret to the `aerion-creds` shim binary's JSON output (keys: `google_ext_client_id`, `google_ext_client_secret`, `microsoft_ext_client_id`). See [`internal/oauth2/config.go`](../internal/oauth2/config.go) loadFromShim function.
3. Optionally pass via ldflags at build time: `-X 'github.com/hkdb/aerion/internal/oauth2.GoogleExtClientID=...'`

Once the shim publishes the new keys, `ClientConfigForID("google-extensions")` starts returning configured credentials and the Auth Broker routes extension scope requests to that client.

### Mapping legacy provider names

[`oauth2.ClientConfigIDForProvider(name)`](../internal/oauth2/clientconfig.go) maps legacy provider strings (stored in `oauth_tokens.provider` column) to their default Mail client config:

| Provider name | Maps to |
|---|---|
| `google`, `google-contacts` | `google-mail` |
| `microsoft`, `microsoft-contacts` | `microsoft-mail` |

Used internally for back-compat queries; extension code rarely needs this directly.

---

## UI registration

Phase 1: interfaces only ([`internal/core/api/v1/ui.go`](../internal/core/api/v1/ui.go)). Phase 2a ships the first concrete registry implementation in `internal/extensions/ui/`. The five registration methods all return an `Unregister` func the caller invokes to remove the registration (e.g., on extension disable or shutdown).

### `RegisterRailTab` — Phase 2a (Contacts)

A vertical icon button on the left activity bar. The rail only renders when 2+ extensions are enabled.

```go
unreg, err := core.UI().RegisterRailTab(coreapi.RailTabRequest{
    ExtensionID: "contacts",
    Label:       "Contacts",
    Icon:        "mdi:account-multiple",
    Component:   "ContactsPane",   // Svelte component identifier
    Order:       10,
})
```

### `RegisterAccountSetupHook` — Phase 2a (Contacts)

A panel that appears in the post-account-add flow in `AccountDialog`. See [§ Account-setup hook contract](#account-setup-hook-contract).

```go
unreg, err := core.UI().RegisterAccountSetupHook(coreapi.AccountSetupHookRequest{
    ExtensionID: "contacts",
    Providers:   []string{"google", "microsoft"},
    ButtonLabel: "Also set up your contacts",
    Component:   "AccountContactsHookPanel",
})
```

### `RegisterSettingsTab`, `RegisterContextMenuItem`, `RegisterInboxView` — Phase 3+

Registrations are accepted but no consumer reads them yet. Reserved for future use; design preserved in the v1 interface so extensions can declare intent now.

---

## Account-setup hook contract

The most important contract for extension UX. Mirrors Thunderbird's "Also set up Calendar / Contacts for this account?" flow.

### Backend registration

In your extension's startup wiring:

```go
core.UI().RegisterAccountSetupHook(coreapi.AccountSetupHookRequest{
    ExtensionID: "myext",
    Providers:   []string{"google", "microsoft", "imap"},
    ButtonLabel: "Also set up <feature> for this account",
    Description: "Optional context shown alongside the button",
    Component:   "MyExtAccountHookPanel",  // Svelte component identifier
})
```

`Providers` lists which mail-account provider strings the hook matches. Only hooks whose `Providers` includes the just-added account's provider will be offered to the user.

### Frontend flow

Wired in Phase 2a via [`AccountDialog.svelte`](../frontend/src/lib/components/settings/AccountDialog.svelte):

1. After `AccountDialog.handleSubmit` successfully creates an account, the dialog computes a `provider` string: `oauthCredentials.provider` for OAuth accounts (`"google"` or `"microsoft"`), `"imap"` otherwise.
2. Dialog calls `loadAccountSetupHooks(provider)` ([`extensionRegistry.svelte.ts`](../frontend/src/lib/stores/extensionRegistry.svelte.ts)) which wraps the Wails-bound `App.ListAccountSetupHooksForProvider`. Hooks are returned regardless of enable state — the hook IS the discovery surface that enables the extension.
3. **Zero hooks** → dialog closes. **Non-zero** → dialog renders a "hooks step" UI that dispatches each hook to its registered Svelte component by `hook.component` name (e.g., `"AccountContactsHookPanel"` → [`extensions/contacts/frontend/hooks/AccountContactsHookPanel.svelte`](../extensions/contacts/frontend/hooks/AccountContactsHookPanel.svelte)).
4. Each panel is opt-in: user clicks "Set up" or "Skip". The "Set up" handler runs the extension's onboarding (Phase 2a Contacts: `LinkAccountContactSource` + `SetExtensionEnabled('contacts', true)` + `refreshExtensionRegistry()`).
5. When all panels resolve (set up or skipped), or the user clicks "Skip all", the dialog closes.

The dispatch in `AccountDialog.svelte` is a static `{#if hook.component === '...'}` block. When you add a new hook component, extend that block — don't switch to `<svelte:component>` dynamic mounting (the component identifier is descriptive only).

### Constraints

- Hook panels must NEVER auto-enable extensions or auto-grant scopes. Every action requires an explicit user click.
- Skipping a panel is the explicit default. Closing the dialog mid-wizard is equivalent to skipping.
- Hooks register at `App.Startup` (synchronously, before Wails serves the frontend) so the dialog's query is always race-free.
- Hooks are returned regardless of whether their extension is currently enabled. The hook IS the discovery surface — its "Set up" handler is what enables the extension. Filtering by enabled state would hide first-party features from new users (extensions default to disabled).

---

## Lifecycle

### What runs at `App.Startup` regardless of enable state

Per [`context/EXTENSION_ARCHITECTURE.md`](../context/EXTENSION_ARCHITECTURE.md), every extension's data store opens eagerly so its schema stays valid across enable/disable cycles:

```go
// In app/app.go Startup:
a.extContactsStore = extcontacts.NewStore(a.paths.Data)  // applies migrations
a.authBroker      = extauth.NewBroker(a.credStore, a.oauth2Manager)
// ...
```

`NewStore` opens the file (creating it if needed) and applies any pending migrations. The first launch after installing a new Aerion build performs the migrations transparently.

### What runs only when enabled

Background services (sync schedulers, IDLE managers, event publishers) start only when the extension is enabled. The architecture doc shows the pattern:

```go
if a.isExtensionEnabled("calendar") {
    a.calendarScheduler = calendar.NewScheduler(a.calendarStore, a.coreAPI)
    a.calendarScheduler.Start()
}
```

Wails-bound methods on disabled extensions return EMPTY results (no error). The frontend can always call methods without checking enabled state; nothing happens if the extension is off.

### Enable / disable

User-facing enable/disable goes through `App.SetExtensionEnabled(name, enabled)` ([§ Wails-bound surface](#wails-bound-surface)). The host is responsible for starting/stopping the extension's background services in response to the flag changing. Phase 1 ships the flag; full lifecycle wiring lands when each extension ships its own background services.

---

## Settings keys

[`internal/settings/store.go`](../internal/settings/store.go) ships the canonical key constants:

```go
const (
    KeyExtensionCalendarEnabled = "extension_calendar_enabled"
    KeyExtensionContactsEnabled = "extension_contacts_enabled"
)
```

Format: `extension_<name>_enabled`. All extensions default to `false`. Helpers:

```go
func (s *Store) IsExtensionEnabled(name string) (bool, error)
func (s *Store) SetExtensionEnabled(name string, enabled bool) error
```

When you ship a new extension, add its key constant alongside the existing two. Use the generic `name`-based helpers — don't write a typed `Get/SetXxxEnabled` per extension.

---

## Wails-bound surface

The frontend calls these via the generated Wails bindings at `frontend/wailsjs/go/app/App.{js,d.ts}`. After modifying any Wails-bound method on `*App`, run `make generate` to regenerate the bindings.

| Method | Purpose |
|---|---|
| `App.IsExtensionEnabled(name string) (bool, error)` | Read the extension's enabled flag |
| `App.SetExtensionEnabled(name string, enabled bool) error` | Write the enabled flag (frontend triggers from Settings UI) |
| `App.LogFrontend(level, message string)` | Bridge for frontend logging — appears in the same zerolog stream as backend logs with `component=frontend`. Levels: `debug|info|warn|error`. Unknown levels fall through to info. |
| `App.ListEnabledExtensions() ([]string, error)` | All currently-enabled extension names (iterates `settings.AllExtensionKeys`). The frontend rail renders when `len() >= 1` (one enabled extension + always-on Mail = two rail items to switch between). |
| `App.ListExtensionRailTabs() ([]v1.RailTabRequest, error)` | Rail tabs for currently-enabled extensions only. Source: [`app/extension_ui.go`](../app/extension_ui.go). |
| `App.ListAccountSetupHooksForProvider(provider string) ([]v1.AccountSetupHookRequest, error)` | Hooks matching a provider, returned regardless of enable state (hooks are the discovery surface that enables an extension). Called by `AccountDialog.svelte` after a new account is created. |
| `App.ListExtensions() ([]app.ExtensionInfo, error)` | Full extension listing for Settings → Extensions tab. Returns manifest fields + current `enabled` state per extension. Iterates `a.knownExtensions`. Source: [`app/extension_ui.go`](../app/extension_ui.go). |
| `App.ListContactsForBrowse(query, sourceID string, limit, offset int) ([]v1.Contact, error)` | Contacts extension browse — wraps `extcontacts.API.ListContacts`. Returns `nil` when Contacts is disabled. Source: [`app/extension_contacts.go`](../app/extension_contacts.go). |
| `App.GetContactDetail(emailOrID string) (*v1.Contact, error)` | Contacts extension single-contact lookup — wraps `extcontacts.API.GetContact`. Returns `nil` when Contacts is disabled. |

### Frontend logger

In any Svelte component or TS file:

```ts
import { logger } from '$lib/logger'

logger.debug('user clicked send')
logger.info('extension contacts: sync started')
logger.warn('extension contacts: source unreachable')
logger.error(`extension contacts: failed: ${err}`)
```

Fire-and-forget — never throws into caller. See [`frontend/src/lib/logger.ts`](../frontend/src/lib/logger.ts).

---

## Testing conventions

Patterns established in Phase 1:

### Interface compile-tests

[`internal/core/api/v1/types_test.go`](../internal/core/api/v1/types_test.go) defines a `stubCore struct{}` that implements EVERY interface in the package with stub methods. The test simply assigns it: `var c Core = stubCore{}`. This compiles only when every interface signature is still satisfied — drift surfaces immediately.

When you ADD a method to an interface in `coreapi`, update `stubCore` in the same commit.

### Real-store integration tests

[`internal/extensions/store_test.go`](../internal/extensions/store_test.go), [`internal/extensions/auth/broker_test.go`](../internal/extensions/auth/broker_test.go): open a real SQLite via `t.TempDir()` + `database.Open`, exercise the API, assert on results. No mocking of the credentials store or DB.

### Auth broker test pattern

The broker test ([`internal/extensions/auth/broker_test.go`](../internal/extensions/auth/broker_test.go)) sets up a temp DB + real `credentials.Store` + real `oauth2.Manager` (which doesn't fire its OAuth flow without a UI). Then it inserts test tokens directly via `credStore.SetOAuthTokens`. Useful for: scope coverage check, `ErrAdditionalConsentRequired` path, 401 refresh (when the test server is wired).

When you write an extension that uses the broker, mirror this pattern.

### Don't mock; use the real store

Aerion's testing style is integration-flavored: a real SQLite at `t.TempDir()` is fast enough (~10ms per open) and exercises actual SQL behavior. Avoid mock layers unless the dependency is genuinely external (an HTTP server — use `httptest.Server`).

---

## Frontend conventions

### Where Svelte components live

| Area | Path |
|---|---|
| Extension rail (host UI) | [`frontend/src/lib/components/rail/`](../frontend/src/lib/components/rail) |
| Settings → Extensions tab (host UI) | [`frontend/src/lib/components/settings/ExtensionsTab.svelte`](../frontend/src/lib/components/settings/ExtensionsTab.svelte) |
| Contacts extension components | [`extensions/contacts/frontend/components/`](../extensions/contacts/frontend/components) |
| Contacts extension stores | [`extensions/contacts/frontend/stores/`](../extensions/contacts/frontend/stores) |
| Contacts account-setup hook panel | [`extensions/contacts/frontend/hooks/`](../extensions/contacts/frontend/hooks) |
| New extensions | `extensions/<name>/frontend/{components,stores,hooks}/` |

Extension-specific UI lives under `extensions/<name>/frontend/`, NOT under `frontend/src/lib/components/`. Only host-owned UI (rail, settings dialog wiring) stays in `frontend/src/`. Keep new files under ~300 LOC.

Rail switching is bound to `Ctrl+Tab` / `Ctrl+Shift+Tab` in [`App.svelte`](../frontend/src/App.svelte). The cycle order matches the rendered rail (Mail first, then enabled extensions in `Order` ASC). See [`docs/KEYBOARD_SHORTCUTS.md`](KEYBOARD_SHORTCUTS.md) for the full shortcut reference.

### Generated Wails bindings

For files inside `frontend/src/`, use relative paths:

```ts
// @ts-ignore - wailsjs bindings
import { ListContactsForBrowse } from '../../../wailsjs/go/app/App'
```

For files inside `extensions/<name>/frontend/`, use the `$wailsjs` alias:

```ts
// @ts-ignore - wailsjs bindings
import { ListContactsForBrowse } from '$wailsjs/go/app/App'
// @ts-ignore - wailsjs bindings
import type { v1 } from '$wailsjs/go/models'
```

The `@ts-ignore` lines stay mandatory in both locations — the generated `.d.ts` files don't carry TS-friendly path aliases.

### Stores

[`frontend/src/lib/stores/extensionRegistry.svelte.ts`](../frontend/src/lib/stores/extensionRegistry.svelte.ts) — frontend cache of enabled extensions and rail tabs. Exposes:

```ts
extensionRegistry.enabled       // string[]
extensionRegistry.railTabs      // v1.RailTabRequest[]
extensionRegistry.railVisible   // boolean (true when length >= 1 — Mail + 1 extension)
extensionRegistry.isEnabled(name)
refreshExtensionRegistry()      // call after enable/disable toggle
loadAccountSetupHooks(provider) // returns v1.AccountSetupHookRequest[]
```

Call `refreshExtensionRegistry()` after `SetExtensionEnabled` so the rail/hooks reflect the new state.

### Active-extension state

Persisted via [`uiState.svelte.ts`](../frontend/src/lib/stores/uiState.svelte.ts) field `activeExtension`:

```ts
import { getActiveExtension, setActiveExtension } from '$lib/stores/uiState.svelte'

const current = getActiveExtension()  // 'mail' | 'contacts' | …
setActiveExtension('contacts')        // debounced save to backend
```

The default is `'mail'`. Switching does NOT clear mail selection (folder/thread state); flipping back to Mail restores the previous mail context exactly.

### Rail-tab component contract

Rail tabs are declared by the backend (`coreapi.RailTabRequest`); the frontend renders them via [`ExtensionRail.svelte`](../frontend/src/lib/components/rail/ExtensionRail.svelte). Each tab needs:

- `extensionId` — the canonical extension name (must match `settings.AllExtensionKeys`)
- `label` — display string (no i18n keys yet — Phase 2a uses plain English)
- `icon` — iconify identifier (e.g., `mdi:account-multiple`)
- `component` — Svelte component identifier; App.svelte switches on `extensionId` to pick the matching component to render

### Slot pattern

The "slot" is a conditional in [`App.svelte`](../frontend/src/App.svelte):

```svelte
{#if getActiveExtension() === 'contacts'}
  <ContactsPane />
{:else}
  <!-- mail layout -->
{/if}
```

When adding a new extension, extend this `if`/`else if` block. Don't refactor it into a dynamic Svelte `<svelte:component>` mount — the component identifier in `RailTabRequest` is descriptive only; the host owns the static dispatch table.

### Vite + tsconfig aliases

Two aliases are configured in [`frontend/vite.config.ts`](../frontend/vite.config.ts) and [`frontend/tsconfig.json`](../frontend/tsconfig.json):

| Alias | Resolves to | Used by |
|---|---|---|
| `$extensions/*` | `<repo>/extensions/*` | Host (App.svelte, AccountDialog.svelte) importing extension Svelte components |
| `$wailsjs/*` | `<repo>/frontend/wailsjs/*` | Extension Svelte/TS files importing generated Wails bindings (without deep `../` chains) |

Because extension files live outside `frontend/`, Rollup's default node-modules walking doesn't find `frontend/node_modules`. Shared npm dependencies (currently `@iconify/svelte`) are aliased explicitly in `vite.config.ts` to point back at the host's `node_modules`. Add new entries to the alias list as extensions pull in additional npm packages.

`tsconfig.json` includes `../extensions/**/frontend/**/*.{ts,svelte}` in its `include` array so `svelte-check` validates extension code alongside host code. Explicit `paths` entries (`@iconify/*`, `svelte`, `svelte/*`) keep TypeScript's type resolution pointing at the host's `node_modules`.

---

## Extension UI Kit

The kit at [`frontend/src/lib/components/kit/`](../frontend/src/lib/components/kit) is the **SDK** extensions compose their UI from. Theme tokens, keyboard navigation, density, accent-bar selection, and avatar palette are all baked in — your extension provides data and callbacks, the kit owns rendering.

### Why an SDK (not a refactor)

The kit is **standalone**. Mail UI (`MessageList`, `Sidebar`, `ConversationViewer`) is independent and is NOT refactored to share components with the kit. Code duplication between the kit and mail is the explicit trade-off: Mail has real users and 1700+ LOC components tangled with sync state / S/MIME / PGP / drafts — touching them to serve extension consistency is regression-risk for no user benefit. The kit copies the good patterns (avatar color hash, density modes, j/k navigation) from mail into greenfield components that consume the same theme tokens.

Visual consistency is preserved at the **theme layer**, not the JS layer. The kit's `Avatar` uses the same `.avatar-1..14` CSS classes (defined in [`frontend/src/themes/_utilities.css`](../frontend/src/themes/_utilities.css)) that mail's avatar uses — so the colors match even though the hash function lives in two files. Same applies to all theme tokens (`bg-muted`, `border-border`, `text-foreground`, `bg-accent`, `text-primary`).

This SDK pattern is anchored in [the lightweight-by-default motto](../README.md) — Aerion remains a simple email client for users who don't enable extensions, and extensions opt-in to features at the cost of weight. Mail must never carry kit overhead.

### Keyboard bridge

Shortcut KEY definitions live in [`frontend/src/lib/keyboard/shortcuts.ts`](../frontend/src/lib/keyboard/shortcuts.ts) — a **single source of truth** for "what key combo matches what action." Both mail's handler (`App.svelte`) and kit components import the same predicates (`KEY.LIST_NEXT`, `KEY.LIST_PREV`, `KEY.LIST_OPEN`, etc.) and reference them via `if (KEY.LIST_NEXT(e)) { ... }`.

The **implementations differ per layer** — mail dispatches via concrete component refs; kit components handle their own events locally via `tabindex=0` + DOM `keydown` listener + `e.stopPropagation()`. The bridge is the file of predicates, not shared dispatch logic.

**Rebinding a key**: change the predicate in `shortcuts.ts`. Both mail and any kit consumer pick up the new binding automatically.

**Active-extension guard**: when an extension is the active rail pane, mail-domain shortcuts (Ctrl+R reply, Ctrl+K archive, Ctrl+J spam, Ctrl+L load-images, Ctrl+U mark-read, Ctrl+A, Ctrl+S, Ctrl+F) no-op via an `isMailActive()` check in `App.svelte`. Global shortcuts (Ctrl+Q quit, Ctrl+N compose, Ctrl+Tab rail-switch) fire regardless. Kit's keydown handlers run first when DOM-focused, so they see the events before the global handler does.

### Components

#### `Avatar` — colored initials circle

[`frontend/src/lib/components/kit/Avatar.svelte`](../frontend/src/lib/components/kit/Avatar.svelte)

```svelte
<Avatar email={contact.email} name={contact.name} density="standard" />
```

| Prop | Type | Notes |
|---|---|---|
| `email` | `string` | Color-hash seed. Same email → same color across mail and the kit. |
| `name` | `string?` | Initials source; falls back to email. |
| `density` | `'micro' \| 'compact' \| 'standard' \| 'large'` | Sizes: 24px / 28px / 32px / 40px. |
| `size` | `number?` | Override the density-derived pixel size. |

Inside the kit, treat `density` as the standard prop; only override `size` when a specific layout demands it.

#### `ListPane` + `ListRow` — keyboard-navigable list

[`frontend/src/lib/components/kit/ListPane.svelte`](../frontend/src/lib/components/kit/ListPane.svelte) and [`ListRow.svelte`](../frontend/src/lib/components/kit/ListRow.svelte)

```svelte
<ListPane
  items={contacts}
  selectedId={selected}
  focusSlot="messageList"
  label="Contacts"
  onSelect={(id) => select(id)}
>
  {#snippet row(c, { selected })}
    <ListRow {selected} onclick={() => select(c.id)}>
      <Avatar email={c.email} name={c.name} />
      <span class="flex flex-col flex-1 min-w-0">
        <span class="font-medium truncate">{c.name}</span>
        <span class="text-xs text-muted-foreground truncate">{c.email}</span>
      </span>
    </ListRow>
  {/snippet}

  {#snippet empty()}
    <p class="m-4 text-sm text-muted-foreground">No items.</p>
  {/snippet}
</ListPane>
```

**`ListPane` owns:**
- j/k/Up/Down navigation (predicates from `shortcuts.ts`)
- Enter to activate (`onActivate ?? onSelect`)
- Space to toggle check (when `onToggleCheck` provided)
- Ctrl+A to select all (when `onSelectAll` provided)
- DOM-level focus via `tabindex=0`; registers as the focused pane's slot via `setFocusedPane(focusSlot)` when DOM-focused
- `e.stopPropagation()` when matched so the global handler doesn't double-fire

**Generic over `T extends { id: string }`** — items just need a stable `id`. The `row` snippet renderer decides everything else.

#### `SourceSidebar` + `SourceItem` — sectioned sidebar

[`frontend/src/lib/components/kit/SourceSidebar.svelte`](../frontend/src/lib/components/kit/SourceSidebar.svelte) and [`SourceItem.svelte`](../frontend/src/lib/components/kit/SourceItem.svelte)

```svelte
<SourceSidebar
  title="Contacts"
  sections={[
    { items: builtins },
    { heading: 'Sources', items: userSources },
  ]}
  selectedId={selected}
  onSelect={pick}
>
  {#snippet item(it, { active })}
    <SourceItem icon={it.icon} label={it.label} {active} onclick={() => pick(it.id)} />
  {/snippet}
</SourceSidebar>
```

**`SourceSidebar` owns:**
- Sectioned layout with optional headings
- j/k/Up/Down navigation across the flattened item list
- Enter to re-select current
- DOM-level focus; registers as `'sidebar'` slot by default (override via `focusSlot` prop)

#### `DetailPane` — header/body/empty-state shell

[`frontend/src/lib/components/kit/DetailPane.svelte`](../frontend/src/lib/components/kit/DetailPane.svelte)

```svelte
<DetailPane empty={!contact} emptyIcon="mdi:account-multiple-outline" emptyText="Select a contact.">
  {#snippet header()}
    <Avatar email={contact.email} name={contact.name} density="large" />
    <h1 class="text-xl font-semibold">{contact.name}</h1>
  {/snippet}
  {#snippet body()}
    <dl>...</dl>
  {/snippet}
</DetailPane>
```

Read-only shell — no keyboard ownership. Header is fixed; body scrolls. Empty-state can be customized via snippet or just `emptyIcon`/`emptyText` props.

### Pane focus slots

The kit reuses Aerion's existing pane-focus store at [`frontend/src/lib/stores/keyboard.svelte.ts`](../frontend/src/lib/stores/keyboard.svelte.ts). The slot type is `'sidebar' | 'messageList' | 'viewer'` — those names are kept as-is for backward compatibility with mail's existing focus dispatch. Extension panes register against these same slots:

| Slot | Mail occupant | Kit equivalent |
|---|---|---|
| `'sidebar'` | `Sidebar.svelte` (folder tree) | `SourceSidebar.svelte` |
| `'messageList'` | `MessageList.svelte` | `ListPane.svelte` |
| `'viewer'` | `ConversationViewer.svelte` | `DetailPane.svelte` |

Alt+H/L pane cycling already cycles through these three slot names — when an extension is active, the kit components take focus and the cycling works uniformly with mail.

### Extending the kit

When a future extension needs a primitive that doesn't exist yet (e.g., Calendar's grid view):

1. Add the new component under `frontend/src/lib/components/kit/`.
2. Keep the kit standalone — never import from `frontend/src/lib/components/{list,sidebar,viewer}/` (mail's components). If the kit needs the same pattern mail has, copy it.
3. Reference shared predicates from `shortcuts.ts` for any new keyboard bindings; add new predicates there if needed.
4. Document the component here with prop table + minimal usage example.
5. Verify the lightweight invariant: with the new component built but no extension enabled, htop should show no Aerion/webkit2gtk activity. The kit must be lazily mounted only when an extension is active.

---

## Distribution model

### Today (v0.3.x): static linking

All first-party extensions compile into the single Aerion binary. Extensions live as Go packages under `extensions/<name>/backend/` and Svelte components under `extensions/<name>/frontend/`. The host imports each extension's `Extension` struct directly and calls `Register()` at startup.

This is the simplest model — no IPC, no version coupling — but it means community extensions are impossible without recompiling Aerion. Acceptable for first-party only.

### Future (v0.4+): subprocess + IPC

Aerion is committed to a **pre-compiled subprocess + IPC** model for community extensions. Each community extension will ship as its own Go binary (cross-compiled per platform) launched as a subprocess at startup, communicating with the main app via Unix socket / named pipe (Aerion already does this for the detached composer — same proven path).

Why subprocess and not other options:
- **Go `plugin` package (.so loading)**: requires exact same Go version + same dependency tree as host; Linux/macOS only; no way to unload. Brittle in practice; almost no one ships this way.
- **WASM**: Go-backend WASM (wazero) is still research-grade for this use case. Promising but immature.
- **Embedded scripting (Lua, JS via goja)**: would force re-implementing CalDAV/CardDAV/heavy sync libs. Aerion extensions do real work and need the real Go ecosystem.
- **Subprocess + IPC**: used by VS Code (language servers), Docker (plugins), Hashicorp's `go-plugin`, Sourcegraph. Real process isolation = security. Capability enforcement at the IPC boundary actually means something (extension never sees raw tokens, can't bypass the Auth Broker via reflection).

The current API design is already subprocess-compatible. Nothing in `coreapi v1` references Go module paths, compiled-type names, or in-process pointers:

- `coreapi v1` interfaces → become gRPC / IPC schema (Go interfaces translate cleanly to protobuf services)
- `Auth Broker` → already designed as an opaque "tokens never leave the boundary" wall, perfect for IPC
- Per-extension SQLite → extension owns its own file in either model
- `RailTabRequest.Component: "ContactsPane"` → already a descriptive string, not a compiled type reference
- `Extension.Register(core)` → works as function call (static) AND as subprocess spawn + IPC handshake

What stays in the host even with community extensions: **the Svelte components**. Even Obsidian doesn't let plugins ship React/Svelte components — Obsidian plugins manipulate the DOM directly via its workspace API. Community extensions will register against pre-built UI slots (rail tab, settings tab, a "generic extension pane" that renders state declared over IPC).

### Migration path

Phase 2a (now) writes everything as if subprocess is coming. When v0.4 starts:

1. First-party extensions migrate from in-process function calls to subprocess + IPC. Same `manifest.json`. Same `Register` shape, just via IPC handshake.
2. Once first-party migration is stable, the community-extension installer lands (download tarball → verify manifest + signature → extract to user dir → launch as subprocess).
3. Settings UI grows a "Community extensions" section beneath "Core extensions".

No API rework needed for the v0.4 migration — only the transport changes.

---

## Not yet implemented

Things extensions CANNOT do in v0.3.0. Items marked with a phase have a planned landing window; others are speculative.

### Backend

- `Mail` mutate methods (`MoveMessage`, `Archive`, `Trash`, `SetFlags`, `AppendMessage`) — return `ErrUnimplemented`. Phase 3+ when a real consumer (filter extension) needs them.
- `SubscribeToMailEvents` — `ErrUnimplemented`. Needs a core event-bus wiring first.
- `Contacts.SubscribeToContactEvents` — `ErrUnimplemented`. Same as above.
- `Composer.OpenComposer` with `Attachments` or `ReplyTo` — `ErrUnimplemented`. Mailto-URL-only path. Phase 2+ when a consumer needs richer compose semantics.
- `Auth.IMAPClient` / `Auth.SMTPClient` — `ErrUnimplemented`. Wires when a real consumer needs them (Sieve, delayed-send).
- `Notifications.Show` — interface only. Phase 3+.
- `UI.RegisterSettingsTab`, `RegisterContextMenuItem`, `RegisterInboxView` — registrations accepted but no consumer reads them yet.
- `EventBus.Publish` / `Subscribe` — interface only.

### Frontend

- The extension rail (first ship: Phase 2a, when Contacts becomes the second extension)
- Slot pattern for swapping main pane between Mail and other extensions (Phase 2a)
- Account-setup hook UI in `AccountDialog` (Phase 2a)
- Per-extension settings tab in the Settings dialog (Phase 3+)
- Per-extension context menu items (Phase 3+)

### System

- Community-extension runtime (dynamic loading, manifest verification, capability consent UI) — deferred to v0.4+ once first-party use has stabilized the API surface
- Per-extension capability gating — Phase 1 grants first-party extensions everything; explicit capability checks land when community extensions arrive

---

## Related documents

- [`context/EXTENSION_ARCHITECTURE.md`](../context/EXTENSION_ARCHITECTURE.md) — design rationale (per-DB isolation, enable/disable, lifecycle, frontend slot pattern, OAuth scope migration strategy, Wails v2 constraints)
- [`context/EXTENSION_API_PLAN.md`](../context/EXTENSION_API_PLAN.md) — detailed Cross-Extension API surface design with motivating use cases
- [`context/CARDDAV_IMPLEMENTATION.md`](../context/CARDDAV_IMPLEMENTATION.md) — CardDAV implementation; pattern reference for the Contacts extension
- [`context/DETACHABLE_COMPOSER_IMPLEMENTATION.md`](../context/DETACHABLE_COMPOSER_IMPLEMENTATION.md) — inline + detach pattern (extensions inherit this)
- [`CLAUDE.md`](../CLAUDE.md) — overall codebase guide
