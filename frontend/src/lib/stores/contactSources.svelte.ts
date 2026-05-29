// Contact Sources Store - manages CardDAV/Google/Microsoft contact sources and their sync status
// @ts-ignore - wailsjs path
import {
  GetContactSources,
  GetContactSourceErrors,
  SyncContactSource,
  SyncAllContactSources,
  DeleteContactSource,
  ClearContactSourceError,
  GetLinkedAccountsForContactSync,
  LinkAccountContactSource,
  StartContactsOnlyOAuthFlow,
  CompleteContactSourceOAuthSetup,
  CancelContactSourceOAuthFlow,
} from '../../../wailsjs/go/app/App.js'
// @ts-ignore - wailsjs path
import type { carddav, app } from '../../../wailsjs/go/models'

// Re-export LinkedAccountInfo type for components
export type LinkedAccountInfo = app.LinkedAccountInfo

function createContactSourcesStore() {
  let sources = $state<carddav.Source[]>([])
  let errors = $state<carddav.SourceError[]>([])
  let loading = $state(false)

  async function load() {
    loading = true
    try {
      const [sourcesResult, errorsResult] = await Promise.all([
        GetContactSources(),
        GetContactSourceErrors(),
      ])
      sources = sourcesResult || []
      errors = errorsResult || []
    } catch (err) {
      console.error('Failed to load contact sources:', err)
    } finally {
      loading = false
    }
  }

  async function refresh() {
    await load()
  }

  async function syncSource(sourceId: string) {
    try {
      await SyncContactSource(sourceId)
      // Refresh to get updated sync status
      await load()
    } catch (err) {
      console.error('Failed to sync contact source:', err)
      throw err
    }
  }

  async function syncAll() {
    try {
      await SyncAllContactSources()
      await load()
    } catch (err) {
      console.error('Failed to sync all contact sources:', err)
      throw err
    }
  }

  async function deleteSource(sourceId: string) {
    try {
      await DeleteContactSource(sourceId)
      await load()
    } catch (err) {
      console.error('Failed to delete contact source:', err)
      throw err
    }
  }

  async function clearError(sourceId: string) {
    try {
      await ClearContactSourceError(sourceId)
      await load()
    } catch (err) {
      console.error('Failed to clear contact source error:', err)
      throw err
    }
  }

  // Get email accounts that can be linked for contact sync
  async function getLinkedAccounts(): Promise<app.LinkedAccountInfo[]> {
    try {
      const accounts = await GetLinkedAccountsForContactSync()
      return accounts || []
    } catch (err) {
      console.error('Failed to get linked accounts:', err)
      return []
    }
  }

  // Link an email account as a contact source
  async function linkAccount(accountId: string, name: string, syncInterval: number) {
    try {
      await LinkAccountContactSource(accountId, name, syncInterval)
      await load()
    } catch (err) {
      console.error('Failed to link account:', err)
      throw err
    }
  }

  // Start OAuth flow for standalone contact source
  async function startOAuthFlow(provider: string) {
    try {
      await StartContactsOnlyOAuthFlow(provider)
    } catch (err) {
      console.error('Failed to start OAuth flow:', err)
      throw err
    }
  }

  // Complete OAuth flow and create source
  async function completeOAuthSetup(name: string, syncInterval: number) {
    try {
      await CompleteContactSourceOAuthSetup(name, syncInterval)
      await load()
    } catch (err) {
      console.error('Failed to complete OAuth setup:', err)
      throw err
    }
  }

  // Cancel OAuth flow
  function cancelOAuthFlow() {
    CancelContactSourceOAuthFlow()
  }

  // isSourceWritable reports whether a CardDAV source has its writable flag
  // enabled. Used by the Contacts extension to gate Edit/Delete buttons on
  // per-source write capability. Unknown sourceIds (e.g., the "aerion" local
  // store, which isn't in this list) return false — callers OR with their
  // own local-source check.
  function isSourceWritable(sourceId: string | undefined): boolean {
    if (!sourceId) return false
    const s = sources.find(s => s.id === sourceId)
    return !!s?.writable
  }

  return {
    get sources() { return sources },
    get errors() { return errors },
    get loading() { return loading },
    get hasErrors() { return errors.length > 0 },
    get errorCount() { return errors.length },

    load,
    refresh,
    syncSource,
    syncAll,
    deleteSource,
    clearError,
    getLinkedAccounts,
    linkAccount,
    startOAuthFlow,
    completeOAuthSetup,
    cancelOAuthFlow,
    isSourceWritable,
  }
}

export const contactSourcesStore = createContactSourcesStore()
