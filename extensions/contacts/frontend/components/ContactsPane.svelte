<script lang="ts">
  import { onMount } from 'svelte'
  import ContactsSidebar from './ContactsSidebar.svelte'
  import ContactList from './ContactList.svelte'
  import ContactDetail from './ContactDetail.svelte'
  import { reloadContacts } from '$extensions/contacts/frontend/stores/contactsView.svelte'

  onMount(() => {
    reloadContacts()
  })

  function handleSourceSelected() {
    reloadContacts()
  }
</script>

<div class="flex flex-1 min-w-0 h-full">
  <ContactsSidebar onSelect={handleSourceSelected} />
  <ContactList />
  <ContactDetail />
</div>
