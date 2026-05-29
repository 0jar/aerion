// Contacts extension's keyboard shortcut predicates.
//
// Lives inside the extension's own directory so the Contacts extension is
// self-contained — adding/removing the extension doesn't require touching
// host-side keyboard files. Mail and the kit consume their shared predicates
// from `$lib/keyboard/shortcuts.ts`; extensions own theirs.
//
// Composable helpers (noMods, ctrlOrMeta, altOnly) are imported from the host
// shortcuts file so the modifier-checking conventions match mail's exactly.
// Predicates here get registered via registerExtensionShortcut at component
// mount; the host's global key handler dispatches them via
// dispatchExtensionShortcut when the Contacts extension is the active rail
// pane.

import { noMods } from '$lib/keyboard/shortcuts'

/** `e` — edit the currently-focused contact. */
export const CONTACT_EDIT = (e: KeyboardEvent): boolean =>
  e.key === 'e' && noMods(e)

export const KEY = {
  CONTACT_EDIT,
}
