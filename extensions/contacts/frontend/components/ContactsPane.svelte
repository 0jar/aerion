<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import ContactsSidebar from './ContactsSidebar.svelte'
  import ContactList from './ContactList.svelte'
  import ContactDetail from './ContactDetail.svelte'
  import AddContactDialog from './AddContactDialog.svelte'
  import ContactEditDialog from './ContactEditDialog.svelte'
  import { contactsView, reloadContacts, selectSource, selectContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { registerExtensionShortcut } from '$lib/stores/extensionShortcuts.svelte'
  import { KEY } from '$extensions/contacts/frontend/keyboard/shortcuts'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  onMount(() => {
    reloadContacts()
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
    // Local-only edit while we wait for CardDAV/OAuth write paths (2b.2.b / 2b.3).
    if (contact.sourceId !== 'aerion') return
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
