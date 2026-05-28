// View-local state for the Contacts extension's browse UI. Source selection,
// search query, and selected-row id are intentionally session-local — none of
// these need to survive across app launches.

// @ts-ignore - wailsjs bindings
import { ListContactsForBrowse, GetContactDetail } from '$wailsjs/go/app/App'
// @ts-ignore - wailsjs bindings
import type { v1 } from '$wailsjs/go/models'

// "" → merged listing
// "local" → core local contacts
// <uuid> → CardDAV source
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
