<script lang="ts">
  import DetailPane from '$lib/components/kit/DetailPane.svelte'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import { Button } from '$lib/components/ui/button'
  import ConfirmDialog from '$lib/components/kit/ConfirmDialog.svelte'
  import Icon from '@iconify/svelte'
  import { contactsView, deleteLocalContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  // Edit-dialog state lives in ContactsPane (hoisted so the 'e' keyboard
  // shortcut can open it from anywhere within the pane). The button below
  // calls onEdit; ContactsPane owns the dialog itself.
  interface Props {
    onEdit?: (contact: v1.Contact) => void
  }
  let { onEdit }: Props = $props()

  let contact = $derived(contactsView.detail)
  let primaryEmail = $derived(contact && contact.emails && contact.emails.length > 0 ? contact.emails[0] : '')

  // Phase 2b.1: edit + delete enabled only for local (sent-recipient) contacts.
  // CardDAV / Google / Microsoft sources gain write capability in 2b.2 / 2b.3
  // alongside the provider-specific write paths.
  let isLocal = $derived(contact?.sourceId === 'aerion')

  let showDeleteConfirm = $state(false)
  let deleting = $state(false)

  async function copyEmail(email: string) {
    try {
      await navigator.clipboard.writeText(email)
      toasts.success(`Copied ${email}`)
    } catch {
      toasts.error('Failed to copy')
    }
  }

  function handleKeydown(e: KeyboardEvent, email: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      e.stopPropagation()
      copyEmail(email)
    }
  }

  async function confirmDelete() {
    if (!contact) return
    deleting = true
    try {
      await deleteLocalContact(contact.id)
      toasts.success('Contact deleted')
    } catch (err) {
      console.error('Failed to delete contact:', err)
      toasts.error(`Failed to delete: ${(err as Error)?.message ?? err}`)
    } finally {
      deleting = false
    }
  }
</script>

<DetailPane
  empty={!contact}
  emptyIcon="mdi:account-multiple-outline"
  emptyText="Select a contact to view details."
>
  {#snippet header()}
    {#if contact}
      <Avatar email={primaryEmail} name={contact.name} density="large" />
      <h1 class="m-0 text-xl font-semibold text-foreground flex-1 min-w-0 truncate">
        {contact.name || '(unnamed)'}
      </h1>
      {#if isLocal}
        <div class="flex items-center gap-1 flex-shrink-0">
          <Button variant="outline" size="sm" onclick={() => { if (contact) onEdit?.(contact) }}>
            <Icon icon="mdi:pencil" class="w-4 h-4 mr-1" />
            Edit
          </Button>
          <Button
            variant="outline"
            size="sm"
            class="text-destructive hover:text-destructive"
            onclick={() => { showDeleteConfirm = true }}
          >
            <Icon icon="mdi:delete-outline" class="w-4 h-4 mr-1" />
            Delete
          </Button>
        </div>
      {/if}
    {/if}
  {/snippet}

  {#snippet body()}
    {#if contact}
      <dl class="grid grid-cols-[120px_1fr] gap-y-2 gap-x-4">
        <dt class="text-sm text-muted-foreground">Email</dt>
        <dd class="m-0 break-words">
          {#if contact.emailItems && contact.emailItems.length > 0}
            {#each contact.emailItems as item (item.email)}
              <div class="flex items-baseline gap-2">
                <span
                  role="button"
                  tabindex="0"
                  class="text-primary hover:underline cursor-pointer"
                  title="Click to copy"
                  onclick={(e) => { e.stopPropagation(); copyEmail(item.email) }}
                  onkeydown={(e) => handleKeydown(e, item.email)}
                >{item.email}</span>
                {#if item.type}
                  <span class="text-xs text-muted-foreground uppercase">{item.type}</span>
                {/if}
                {#if item.isPrimary}
                  <span class="text-xs text-primary">primary</span>
                {/if}
              </div>
            {/each}
          {/if}
          {#if (!contact.emailItems || contact.emailItems.length === 0) && contact.emails && contact.emails.length > 0}
            {#each contact.emails as email (email)}
              <div>
                <span
                  role="button"
                  tabindex="0"
                  class="text-primary hover:underline cursor-pointer"
                  title="Click to copy"
                  onclick={(e) => { e.stopPropagation(); copyEmail(email) }}
                  onkeydown={(e) => handleKeydown(e, email)}
                >{email}</span>
              </div>
            {/each}
          {/if}
        </dd>

        {#if contact.phones && contact.phones.length > 0}
          <dt class="text-sm text-muted-foreground">Phone</dt>
          <dd class="m-0 break-words text-foreground">
            {#each contact.phones as p (p.number + (p.type ?? ''))}
              <div class="flex items-baseline gap-2">
                <span>{p.number}</span>
                {#if p.type}
                  <span class="text-xs text-muted-foreground uppercase">{p.type}</span>
                {/if}
                {#if p.isPrimary}
                  <span class="text-xs text-primary">primary</span>
                {/if}
              </div>
            {/each}
          </dd>
        {/if}

        {#if contact.addresses && contact.addresses.length > 0}
          <dt class="text-sm text-muted-foreground">Address</dt>
          <dd class="m-0 break-words text-foreground space-y-2">
            {#each contact.addresses as a, i (i)}
              <div>
                {#if a.type}
                  <span class="text-xs text-muted-foreground uppercase mr-2">{a.type}</span>
                {/if}
                <span>
                  {[a.street, a.city, a.region, a.postcode, a.country].filter(Boolean).join(', ')}
                </span>
              </div>
            {/each}
          </dd>
        {/if}

        {#if contact.org}
          <dt class="text-sm text-muted-foreground">Organization</dt>
          <dd class="m-0 break-words text-foreground">{contact.org}</dd>
        {/if}

        {#if contact.title}
          <dt class="text-sm text-muted-foreground">Title</dt>
          <dd class="m-0 break-words text-foreground">{contact.title}</dd>
        {/if}

        {#if contact.bday}
          <dt class="text-sm text-muted-foreground">Birthday</dt>
          <dd class="m-0 break-words text-foreground">{contact.bday}</dd>
        {/if}

        {#if contact.nickname}
          <dt class="text-sm text-muted-foreground">Nickname</dt>
          <dd class="m-0 break-words text-foreground">{contact.nickname}</dd>
        {/if}

        {#if contact.urls && contact.urls.length > 0}
          <dt class="text-sm text-muted-foreground">URL</dt>
          <dd class="m-0 break-words text-foreground">
            {#each contact.urls as u (u.url + (u.type ?? ''))}
              <div class="flex items-baseline gap-2">
                <a href={u.url} target="_blank" rel="noopener noreferrer" class="text-primary hover:underline">{u.url}</a>
                {#if u.type}
                  <span class="text-xs text-muted-foreground uppercase">{u.type}</span>
                {/if}
              </div>
            {/each}
          </dd>
        {/if}

        {#if contact.impps && contact.impps.length > 0}
          <dt class="text-sm text-muted-foreground">IM</dt>
          <dd class="m-0 break-words text-foreground">
            {#each contact.impps as i (i.handle + (i.type ?? ''))}
              <div class="flex items-baseline gap-2">
                <span>{i.handle}</span>
                {#if i.type}
                  <span class="text-xs text-muted-foreground uppercase">{i.type}</span>
                {/if}
              </div>
            {/each}
          </dd>
        {/if}

        {#if contact.categories && contact.categories.length > 0}
          <dt class="text-sm text-muted-foreground">Categories</dt>
          <dd class="m-0 break-words text-foreground">
            <div class="flex flex-wrap gap-1">
              {#each contact.categories as cat (cat)}
                <span class="text-xs px-2 py-0.5 rounded-full bg-muted text-muted-foreground">{cat}</span>
              {/each}
            </div>
          </dd>
        {/if}

        {#if contact.note}
          <dt class="text-sm text-muted-foreground">Note</dt>
          <dd class="m-0 break-words text-foreground whitespace-pre-wrap">{contact.note}</dd>
        {/if}

        {#if contact.sourceId}
          <dt class="text-sm text-muted-foreground">Source</dt>
          <dd class="m-0 break-words text-foreground">{contact.sourceId}</dd>
        {/if}

        <dt class="text-sm text-muted-foreground">Last updated</dt>
        <dd class="m-0 text-foreground">
          {contact.updatedAt ? new Date(contact.updatedAt).toLocaleString() : '—'}
        </dd>
      </dl>
    {/if}
  {/snippet}
</DetailPane>

<ConfirmDialog
  bind:open={showDeleteConfirm}
  title="Delete this contact?"
  description={contact ? `${contact.name || primaryEmail || '(unnamed)'} will be removed from your local contacts. Mail you've already sent to this address is not affected.` : ''}
  confirmLabel="Delete"
  cancelLabel="Cancel"
  variant="destructive"
  loading={deleting}
  onConfirm={confirmDelete}
/>
