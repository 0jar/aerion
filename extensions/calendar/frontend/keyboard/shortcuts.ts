// Calendar extension's keyboard shortcut predicates.
//
// Mirrors the contacts extension's pattern — self-contained inside the
// extension's directory, registered at component mount via
// registerExtensionShortcut. The host's global handler dispatches via
// dispatchExtensionShortcut when Calendar is the active rail pane.

import { noMods, ctrlOrMeta } from '$lib/keyboard/shortcuts'

/** `t` — jump the calendar view to today. */
export const CALENDAR_TODAY = (e: KeyboardEvent): boolean =>
  e.key === 't' && noMods(e)

/** `←` — navigate to the previous view-unit (prev month / week / day). */
export const CALENDAR_PREV = (e: KeyboardEvent): boolean =>
  e.key === 'ArrowLeft' && noMods(e)

/** `→` — navigate to the next view-unit (next month / week / day). */
export const CALENDAR_NEXT = (e: KeyboardEvent): boolean =>
  e.key === 'ArrowRight' && noMods(e)

/** `Ctrl/Cmd+R` — trigger a sync of all sources. */
export const CALENDAR_SYNC = (e: KeyboardEvent): boolean =>
  e.key === 'r' && ctrlOrMeta(e) && !e.shiftKey && !e.altKey

export const KEY = {
  CALENDAR_TODAY,
  CALENDAR_PREV,
  CALENDAR_NEXT,
  CALENDAR_SYNC,
}
