package v1

// Manifest is the metadata for one extension. Every first-party extension
// ships a manifest.json at its repo root (e.g., extensions/contacts/manifest.json)
// embedded into the binary via go:embed. Community extensions (v0.4+) will
// ship the same manifest.json at the root of their distribution tarball.
//
// Field choices favor subprocess+IPC distribution (v0.4+ commitment): no Go
// import paths, no compiled-type references, no host-coupled fields. The
// host reads the manifest before deciding whether to load an extension.
type Manifest struct {
	ID               string   `json:"id"`               // canonical extension id (matches settings.AllExtensionKeys)
	Name             string   `json:"name"`             // user-facing display name
	Version          string   `json:"version"`          // semver
	Description      string   `json:"description"`      // 1-2 sentence summary shown in Settings
	Author           string   `json:"author"`
	MinAerionVersion string   `json:"minAerionVersion"` // semver — host refuses to load if lower
	Capabilities     []string `json:"capabilities"`     // coarse capabilities; see below
}

// Capability is a coarse permission string an extension declares in its
// manifest. The host's runtime checks (Auth Broker scope checks, UI registry
// validation) verify finer-grained access at the API boundary; capabilities
// in the manifest are for upfront consent UI ("This extension wants to: read
// your contacts, add a rail tab").
//
// Known capability strings (treated as opaque otherwise so the set can grow
// without breaking older hosts):
//
//	"contacts.read"             — read core contacts and CardDAV-synced contacts
//	"contacts.write"            — write to CardDAV/Google/Microsoft (v0.4+)
//	"mail.read"                 — read messages and folders
//	"mail.write"                — mutate messages (move/archive/flag)
//	"compose"                   — open the composer with a prefilled draft
//	"ui.rail-tab"               — register a rail-tab UI surface
//	"ui.settings-tab"           — register a settings-tab UI surface
//	"ui.account-setup-hook"     — register a hook in the post-account-add flow
//	"ui.context-menu"           — register context-menu items
//	"ui.inbox-view"             — register an alternate inbox rendering
//	"storage"                   — open per-extension SQLite + KV
//	"network"                   — make outbound HTTP requests via Auth Broker
type Capability = string

// Extension is the Go-side handle every first-party extension exposes from
// its package. Community extensions (v0.4+) won't satisfy this interface
// directly (they're separate subprocesses), but their Register handshake
// over IPC will mirror these two methods.
//
// Register is called once per Aerion process lifetime, at startup, regardless
// of whether the extension is currently enabled. This matches the
// architecture-doc rule that descriptive UI registrations (rail tab, hooks)
// persist across enable/disable cycles. Active behaviors that depend on
// enabled state (sync schedulers, background work) are gated separately by
// IsExtensionEnabled checks inside Register, not by skipping Register.
//
// The returned Unregister removes all of the extension's registrations.
// Called by the host on process shutdown.
type Extension interface {
	// Manifest returns the extension's parsed manifest. Implementations
	// typically embed manifest.json via go:embed and parse once.
	Manifest() Manifest

	// Register wires the extension's UI surfaces (rail tabs, hooks, etc.)
	// and returns an Unregister func that tears them down.
	Register(core Core) (Unregister, error)
}
