package v1

import (
	"errors"
	"fmt"
)

// Sentinel errors. Extensions should errors.Is(err, v1.ErrXxx) to detect them.
var (
	// ErrDisabled is returned by API methods when the underlying extension or
	// feature is disabled. Callers should treat this as a benign "feature off"
	// signal rather than a failure.
	ErrDisabled = errors.New("extension or feature is disabled")

	// ErrCapabilityDenied is returned when an extension calls an API it has
	// not been granted the capability for. For first-party extensions in
	// Phase 1, this should never occur (all-or-nothing grants).
	ErrCapabilityDenied = errors.New("capability denied")

	// ErrAccountNotFound is returned when an API call references an account
	// ID that does not exist (or has been deleted).
	ErrAccountNotFound = errors.New("account not found")

	// ErrUnimplemented is returned by API methods that are scaffolded but
	// not implemented in the current release. Phase 1 returns this for Mail
	// mutators, event subscriptions, the event bus, and UI registrations.
	ErrUnimplemented = errors.New("not implemented in this release")
)

// ErrAdditionalConsentRequired signals that the extension's request needs
// additional OAuth consent from the user before it can succeed. The host (not
// the extension) handles the consent flow and retries.
type ErrAdditionalConsentRequired struct {
	AccountID      string
	ClientConfigID ClientConfigID
	MissingScopes  []AuthScope
}

func (e *ErrAdditionalConsentRequired) Error() string {
	return fmt.Sprintf("additional consent required for account %s under %s: %d scope(s) missing",
		e.AccountID, e.ClientConfigID, len(e.MissingScopes))
}
