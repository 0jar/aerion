<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import ContactsSidebar from './ContactsSidebar.svelte'
  import ContactList from './ContactList.svelte'
  import ContactDetail from './ContactDetail.svelte'
  import AddContactDialog from './AddContactDialog.svelte'
  import ContactEditDialog from './ContactEditDialog.svelte'
  import { contactsView, reloadContacts, selectSource, selectContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { contactSourcesStore } from '$lib/stores/contactSources.svelte'
  import { toasts } from '$lib/stores/toast'
  import { registerExtensionShortcut } from '$lib/stores/extensionShortcuts.svelte'
  import { KEY } from '$extensions/contacts/frontend/keyboard/shortcuts'
  // @ts-ignore - wailsjs bindings
  import { EventsOn } from '$wailsjs/runtime/runtime'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  // Conflict events fire when a CardDAV write loses the optimistic-concurrency
  // race (server's ETag changed between read and PUT/DELETE). The Wails
  // backend has already refreshed the local cache from the server before
  // emitting — the UI just needs to toast + re-render.
  let unsubscribeConflict: (() => void) | null = null

  onMount(() => {
    reloadContacts()
    unsubscribeConflict = EventsOn('contacts:conflict', async (payload: { contactId: string; message: string }) => {
      toasts.error(
        'This contact was changed elsewhere. Your edit was discarded — refreshed with the latest version.',
      )
      await reloadContacts()
      if (payload?.contactId && contactsView.selectedContactId === payload.contactId) {
        await selectContact(payload.contactId)
      }
    })
  })

  onDestroy(() => {
    if (unsubscribeConflict) unsubscribeConflict()
  })

  let showAdd = $state(false)

  // Edit-dialog state is hoisted to the pane so the 'e' keyboard shortcut and
  // ContactDetail's Edit button both route through one owner.
  let showEdit = $state(false)
  let editTarget = $state<v1.Contact | null>(null)

  function handleSourceSelected() {
    reloadContacts()
  }

  function openAdd() {
    showAdd = true
  }

  function openEdit(contact: v1.Contact | null) {
    if (!contact) return
    // Open for any writable source — local (always writable) or a CardDAV
    // source that has its writable flag enabled. Google/Microsoft sources
    // are gated to read-only until 2b.3 ships their write paths.
    const writable =
      contact.sourceId === 'aerion' || contactSourcesStore.isSourceWritable(contact.sourceId)
    if (!writable) return
    editTarget = contact
    showEdit = true
  }

  async function handleCreated(id: string) {
    // After a successful Add, switch the sidebar to the "Contacts" sub-source
    // so the user sees the new entry next to other manual ones, then select
    // the new row.
    selectSource('local:manual')
    await reloadContacts()
    await selectContact(id)
  }

  // 'e' opens the edit dialog for the currently-selected contact. Wired via
  // the extension-shortcut registry: App.svelte's global key handler calls
  // dispatchExtensionShortcut, which only invokes this when the Contacts
  // extension is the active rail pane (so 'e' on the mail side stays free).
  const unregEdit = registerExtensionShortcut('contacts', KEY.CONTACT_EDIT, () => {
    openEdit(contactsView.detail)
  })
  onDestroy(unregEdit)
</script>

<div class="flex flex-1 min-w-0 h-full">
  <ContactsSidebar onSelect={handleSourceSelected} />
  <ContactList onAdd={openAdd} />
  <ContactDetail onEdit={openEdit} />
</div>

<AddContactDialog bind:open={showAdd} onCreated={handleCreated} />
<ContactEditDialog bind:open={showEdit} contact={editTarget} />
