// Extension-owned contact-sources store. Mirrors the narrow subset of the
// core `$lib/stores/contactSources.svelte` interface the extension actually
// uses (sources cache, isSourceWritable, linkAccount, load) — but every
// backend call goes through the Contacts_* prefixed bridge methods rather
// than reaching into core's contact-source Wails methods.
//
// Why a parallel store rather than reusing core's: the core store is mail-
// side (it backs the Contacts settings tab's source list + the
// ContactSourceDialog used for mail autocomplete). Extension code reaching
// into core's `$lib/stores/contactSources.svelte` directly violates the
// EXTENSIONS.md "no internal/core-store imports" rule. This store gives
// the extension the same API shape while keeping all data flow on the
// extension's bridge surface.
//
// Mail-side `ContactSourceDialog.svelte` is intentionally untouched — it
// continues using the core store. Core code calling core methods unprefixed
// is fine; only extension → core needs to go through the bridge.

// @ts-ignore - wailsjs bindings
import {
  Contacts_ListSources as ListSources,
  Contacts_LinkAccountSource as LinkAccountSource,
} from '$wailsjs/go/app/App'
// @ts-ignore - wailsjs bindings
import type { v1 } from '$wailsjs/go/models'

function createContactSourcesStore() {
  let sources = $state<v1.ContactSource[]>([])
  let loading = $state(false)

  async function load(): Promise<void> {
    loading = true
    try {
      const result = await ListSources()
      sources = result || []
    } catch (err) {
      console.error('Failed to load contact sources:', err)
      sources = []
    } finally {
      loading = false
    }
  }

  // Alias kept for parity with the core store's interface — the extension's
  // single existing consumer (AccountContactsHookPanel) calls
  // `linkAccount(accountId, name, syncInterval)`. After linking, refresh the
  // cached list so subsequent .sources reads see the new source.
  async function linkAccount(accountId: string, name: string, syncInterval: number): Promise<void> {
    await LinkAccountSource(accountId, name, syncInterval)
    await load()
  }

  // Synchronous boolean derived from the cached list. Returns false for
  // unknown ids (the "aerion" local sentinel, OAuth sources not yet
  // writable, etc.) — callers OR with their own local-source check.
  function isSourceWritable(sourceId: string | undefined): boolean {
    if (!sourceId) return false
    const s = sources.find(s => s.id === sourceId)
    return !!s?.writable
  }

  return {
    get sources(): v1.ContactSource[] {
      return sources
    },
    get loading(): boolean {
      return loading
    },
    load,
    linkAccount,
    isSourceWritable,
  }
}

export const contactSourcesStore = createContactSourcesStore()
