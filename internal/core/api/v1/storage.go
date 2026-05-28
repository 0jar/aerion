package v1

// KVStore is a small key-value namespace scoped to one extension. Used for
// tiny config (e.g., "lastSyncToken", "preferredView") that doesn't warrant
// SQL tables. Each extension's main SQLite file is opened separately by its
// own internal/<ext>/store.go.
type KVStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	List(prefix string) ([]string, error)
}

// Storage provides the per-extension KV store. Per-extension SQLite is
// implicit (each extension's internal/<name>/store.go opens its own DB).
type Storage interface {
	KV(extensionID string) KVStore
}
