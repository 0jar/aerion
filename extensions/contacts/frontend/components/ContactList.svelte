<script lang="ts">
  // ContactList — mirrors mail's MessageList toolbar pattern:
  //
  //   [Title  |  Search input (when open)]      [Search]  [Sort]
  //
  // Search bar is HIDDEN by default and toggled open via the search button
  // or Ctrl+S. toggleSearchFocus() is exposed to ListPane's onFocusSearch
  // so the global Ctrl+S handler cycles through the same three states mail
  // uses: closed → focused → closed.
  //
  // Sort is local component state (A-Z / Z-A). Lists are small enough that
  // sorting client-side via $derived is cheaper than a backend round-trip.
  // Filter UI is intentionally omitted in Phase 2a — comes when contacts
  // gains tags/groups in a later phase.

  import Icon from '@iconify/svelte'
  import ListPane from '$lib/components/kit/ListPane.svelte'
  import ListRow from '$lib/components/kit/ListRow.svelte'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import { contactsView, reloadContacts, selectContact, setSearchQuery } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  type SortOrder = 'name-asc' | 'name-desc'

  let showSearch = $state(false)
  let searchInput = $state('')
  // Plain `let` (not $state) — same as App.svelte's component refs. The
  // ref is only read inside event handlers (focus / blur / select / equality
  // check against document.activeElement), never in a reactive context, so
  // making it $state adds overhead without benefit.
  // svelte-ignore non_reactive_update
  let searchInputEl: HTMLInputElement | null = null
  let sortOrder = $state<SortOrder>('name-asc')

  let debounce: ReturnType<typeof setTimeout> | null = null
  function onSearchInput(e: Event) {
    searchInput = (e.currentTarget as HTMLInputElement).value
    if (debounce) clearTimeout(debounce)
    debounce = setTimeout(() => {
      setSearchQuery(searchInput)
      reloadContacts()
    }, 200)
  }

  function handleSearchKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      // Match mail: Enter blurs the input and hands focus to the list so j/k
      // navigation works immediately on filtered results.
      e.preventDefault()
      searchInputEl?.blur()
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      clearSearch()
    }
  }

  function clearSearch() {
    searchInput = ''
    setSearchQuery('')
    showSearch = false
    if (debounce) clearTimeout(debounce)
    reloadContacts()
  }

  // Three-state Ctrl+S toggle (matches MessageList.toggleSearchFocus):
  //   closed                  → open + focus
  //   open but unfocused      → focus
  //   open and focused        → close
  function toggleSearchFocus() {
    if (!showSearch) {
      showSearch = true
      setTimeout(() => {
        searchInputEl?.focus()
        searchInputEl?.select()
      }, 50)
      return
    }
    if (document.activeElement !== searchInputEl) {
      searchInputEl?.focus()
      searchInputEl?.select()
      return
    }
    clearSearch()
  }

  function toggleSort() {
    sortOrder = sortOrder === 'name-asc' ? 'name-desc' : 'name-asc'
  }

  function primaryEmail(c: v1.Contact): string {
    return c.emails && c.emails.length > 0 ? c.emails[0] : ''
  }

  function rowKey(c: v1.Contact): string {
    return (c.name || primaryEmail(c) || '').toLowerCase()
  }

  // Client-side sort of the already-loaded list. Backend handles query
  // filtering; sort is purely a view concern.
  const sortedContacts = $derived.by(() => {
    const items = [...contactsView.contacts]
    const dir = sortOrder === 'name-asc' ? 1 : -1
    items.sort((a, b) => {
      const ka = rowKey(a)
      const kb = rowKey(b)
      if (ka < kb) return -1 * dir
      if (ka > kb) return 1 * dir
      return 0
    })
    return items
  })
</script>

<div class="flex-1 min-w-0 flex flex-col border-r border-border bg-background">
  <!-- Header / toolbar -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-border">
    <div class="flex items-center gap-2 flex-1 min-w-0">
      {#if showSearch}
        <div class="flex items-center gap-1 bg-muted rounded-md px-2 flex-1 min-w-0">
          <Icon icon="mdi:magnify" class="w-4 h-4 text-muted-foreground flex-shrink-0" />
          <input
            bind:this={searchInputEl}
            type="text"
            placeholder="Search contacts..."
            class="bg-transparent border-none outline-none text-sm py-1.5 w-full min-w-[200px] text-foreground"
            value={searchInput}
            oninput={onSearchInput}
            onkeydown={handleSearchKeydown}
          />
          {#if searchInput}
            <button
              onclick={clearSearch}
              class="p-0.5 hover:bg-muted-foreground/20 rounded"
              title="Clear search"
              type="button"
            >
              <Icon icon="mdi:close" class="w-4 h-4 text-muted-foreground" />
            </button>
          {/if}
        </div>
      {:else}
        <h2 class="font-semibold text-foreground truncate">Contacts</h2>
        <span class="text-sm text-muted-foreground flex-shrink-0">
          {contactsView.contacts.length}
        </span>
      {/if}
    </div>
    <div class="flex items-center gap-1 flex-shrink-0">
      <button
        class="p-2 rounded-md hover:bg-muted transition-colors {showSearch ? 'bg-muted' : ''}"
        title={showSearch ? 'Close search' : 'Search'}
        onclick={toggleSearchFocus}
        type="button"
      >
        <Icon icon={showSearch ? 'mdi:close' : 'mdi:magnify'} class="w-5 h-5 text-muted-foreground" />
      </button>
      <button
        class="p-2 rounded-md hover:bg-muted transition-colors"
        title={sortOrder === 'name-asc' ? 'Sort: A → Z' : 'Sort: Z → A'}
        onclick={toggleSort}
        type="button"
      >
        <Icon
          icon={sortOrder === 'name-asc' ? 'mdi:sort-alphabetical-ascending' : 'mdi:sort-alphabetical-descending'}
          class="w-5 h-5 text-muted-foreground"
        />
      </button>
    </div>
  </div>

  <ListPane
    items={sortedContacts}
    selectedId={contactsView.selectedContactId}
    focusSlot="messageList"
    label="Contacts"
    loading={contactsView.loading}
    onSelect={(id) => selectContact(id)}
    onFocusSearch={toggleSearchFocus}
  >
    {#snippet row(c: v1.Contact, { selected })}
      <ListRow {selected} onclick={() => selectContact(c.id)}>
        <Avatar email={primaryEmail(c)} name={c.name} density="standard" />
        <span class="flex flex-col min-w-0 flex-1">
          <span class="font-medium truncate text-foreground">{c.name || primaryEmail(c) || '(unnamed)'}</span>
          {#if primaryEmail(c) && primaryEmail(c) !== c.name}
            <span class="text-xs text-muted-foreground truncate">{primaryEmail(c)}</span>
          {/if}
        </span>
      </ListRow>
    {/snippet}

    {#snippet empty()}
      <p class="m-4 text-sm text-muted-foreground">
        {searchInput ? 'No contacts match your search.' : 'No contacts.'}
      </p>
    {/snippet}
  </ListPane>
</div>
