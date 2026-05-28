package backend

import (
	"github.com/hkdb/aerion/internal/extensions"
)

// migrations is the per-extension migration sequence for the Contacts
// extension's isolated DB. Empty in Phase 1; populated when the extension
// lands in Phase 2.
var migrations = []extensions.Migration{}

// Store wraps the per-extension DB for the Contacts extension. Phase 1
// returns the wrapper; Phase 2 adds methods (CRUD, sync state, etc.).
type Store struct {
	*extensions.Store
}

// NewStore opens the Contacts extension's isolated SQLite DB at
// <dataDir>/extensions/contacts/data.db and applies any pending migrations.
// Called from App.Startup eagerly (whether or not the extension is enabled)
// so the schema stays valid across enable/disable cycles.
func NewStore(dataDir string) (*Store, error) {
	s, err := extensions.OpenStore(dataDir, "contacts", migrations)
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}
