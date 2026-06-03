// Calendar extension-local settings store. Persisted via localStorage —
// pragmatic for a single user-pref string today; the public API of this
// module is shaped to be swappable for a Wails-backed Store when 1G's
// settings dialog lands and the broader extension-settings infrastructure
// follows the SQLite pattern.
//
// Currently holds only the user's display-timezone choice:
//   ''          → auto-detect (fall back to the webview's resolved zone)
//   '<IANA>'    → override formatters and date math to use this zone
//
// Consumers should always read `effectiveTimezone` — it resolves the
// auto-detect fallback so callers don't have to.

import { logger } from '$extensions/calendar/frontend/lib/logger'

const STORAGE_KEY = 'aerion:calendar:displayTimezone'

let displayTimezone = $state<string>('')

// Synchronous module-init read. Safe to run before mount because the
// store module is imported lazily (only when Calendar is the active rail
// pane), and localStorage access is synchronous.
try {
  displayTimezone = window.localStorage.getItem(STORAGE_KEY) ?? ''
} catch (err) {
  logger.warn(`calendarSettings: localStorage read failed: ${err}`)
}

const effectiveTimezone = $derived(
  displayTimezone !== ''
    ? displayTimezone
    : Intl.DateTimeFormat().resolvedOptions().timeZone
)

export const calendarSettings = {
  /** User's stored choice. Empty string = auto-detect. */
  get displayTimezone() { return displayTimezone },

  /** The IANA timezone all formatters + tzMath helpers should use. Resolves
   *  the auto-detect fallback so callers never have to. */
  get effectiveTimezone() { return effectiveTimezone },

  /** Set the user's tz choice. Empty string clears the override. */
  setDisplayTimezone(tz: string) {
    displayTimezone = tz
    try {
      if (tz === '') window.localStorage.removeItem(STORAGE_KEY)
      else window.localStorage.setItem(STORAGE_KEY, tz)
    } catch (err) {
      logger.warn(`calendarSettings: localStorage write failed: ${err}`)
    }
  },
}
