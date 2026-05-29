// View-local state for the Contacts extension's browse UI. Source selection,
// search query, and selected-row id are intentionally session-local — none of
// these need to survive across app launches.

// @ts-ignore - wailsjs bindings
import { ListContactsForBrowse, GetContactDetail, UpdateLocalContact, DeleteLocalContact, CreateLocalContact } from '$wailsjs/go/app/App'
// @ts-ignore - wailsjs bindings
import type { v1 } from '$wailsjs/go/models'

// Source ID values the sidebar can dispatch:
//   ""                  → merged listing across all sources
//   "local"             → all local contacts (manual + collected)
//   "local:manual"      → user-added local contacts (Add Contact UI)
//   "local:collected"   → auto-collected local contacts (sent-mail recipients)
//   <uuid>              → a specific CardDAV source
let selectedSourceId = $state<string>('')
let searchQuery = $state<string>('')
let selectedContactId = $state<string | null>(null)
let contacts = $state<v1.Contact[]>([])
let detail = $state<v1.Contact | null>(null)
let loading = $state<boolean>(false)

export const contactsView = {
  get selectedSourceId(): string {
    return selectedSourceId
  },
  get searchQuery(): string {
    return searchQuery
  },
  get selectedContactId(): string | null {
    return selectedContactId
  },
  get contacts(): v1.Contact[] {
    return contacts
  },
  get detail(): v1.Contact | null {
    return detail
  },
  get loading(): boolean {
    return loading
  },
}

export function selectSource(sourceId: string): void {
  selectedSourceId = sourceId
  selectedContactId = null
  detail = null
  // Caller (ContactsPane) decides when to call reloadContacts().
}

export function setSearchQuery(q: string): void {
  searchQuery = q
}

export async function reloadContacts(limit = 200, offset = 0): Promise<void> {
  loading = true
  try {
    contacts = await ListContactsForBrowse(searchQuery, selectedSourceId, limit, offset) || []
  } catch (err) {
    console.error('Failed to list contacts for browse:', err)
    contacts = []
  } finally {
    loading = false
  }
}

export async function selectContact(id: string | null): Promise<void> {
  selectedContactId = id
  if (!id) {
    detail = null
    return
  }
  try {
    detail = await GetContactDetail(id)
  } catch (err) {
    console.error('Failed to load contact detail:', err)
    detail = null
  }
}

// Rename a local (sent-recipient) contact. Email stays the primary key;
// only the display name is editable for local contacts. Marks the contact
// as user-overridden so auto-collection on future sends preserves the edit.
export async function updateLocalContact(email: string, name: string): Promise<void> {
  await UpdateLocalContact(email, name)
  // Refresh the list + detail view so changes are visible immediately.
  await reloadContacts()
  if (selectedContactId === email) {
    await selectContact(email)
  }
}

// Delete a local (sent-recipient) contact entirely. After deletion the list
// reloads and detail view clears.
export async function deleteLocalContact(email: string): Promise<void> {
  await DeleteLocalContact(email)
  if (selectedContactId === email) {
    selectedContactId = null
    detail = null
  }
  await reloadContacts()
}

// Create a manual local contact (kind='manual'). Returns the new contact's
// id (the normalized email). Throws on conflict — caller (AddContactDialog)
// translates ErrContactExists into a field-level "already exists" message.
//
// Note: this does NOT reload contacts or change the selected source — the
// caller (ContactsPane.handleCreated) controls the post-create UX so the
// dialog can close before the source switch.
export async function createLocalContact(email: string, name: string): Promise<string> {
  return await CreateLocalContact(email, name)
}
