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

import { noMods, ctrlOrMeta } from '$lib/keyboard/shortcuts'

/** `e` — edit the currently-focused contact. */
export const CONTACT_EDIT = (e: KeyboardEvent): boolean =>
  e.key === 'e' && noMods(e)

/** `Ctrl/Cmd+N` — open the new-contact dialog, pre-targeted to the
 *  sidebar-focused addressbook (the dialog's own `autoFillFromSidebar`
 *  reads `contactsView.selectedSourceId` and falls back to local when
 *  the focused source isn't writable). Routed by the extension shortcut
 *  registry before App.svelte's mail-domain switch — only fires when
 *  contacts is the active rail. */
export const CONTACT_NEW = (e: KeyboardEvent): boolean =>
  e.key.toLowerCase() === 'n' && ctrlOrMeta(e) && !e.shiftKey && !e.altKey

export const KEY = {
  CONTACT_EDIT,
  CONTACT_NEW,
}
