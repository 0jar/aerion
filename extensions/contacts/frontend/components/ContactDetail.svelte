<script lang="ts">
  import DetailPane from '$lib/components/kit/DetailPane.svelte'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import { contactsView } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { toasts } from '$lib/stores/toast'

  let contact = $derived(contactsView.detail)
  let primaryEmail = $derived(contact && contact.emails && contact.emails.length > 0 ? contact.emails[0] : '')

  // Match mail's pattern (ConversationViewer.svelte) — click-to-copy instead of
  // a mailto: link. mailto: in the Wails WebKit view results in a dead-link
  // navigation; copy-to-clipboard is the established Aerion behavior.
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
</script>

<DetailPane
  empty={!contact}
  emptyIcon="mdi:account-multiple-outline"
  emptyText="Select a contact to view details."
>
  {#snippet header()}
    {#if contact}
      <Avatar email={primaryEmail} name={contact.name} density="large" />
      <h1 class="m-0 text-xl font-semibold text-foreground">{contact.name || '(unnamed)'}</h1>
    {/if}
  {/snippet}

  {#snippet body()}
    {#if contact}
      <dl class="grid grid-cols-[120px_1fr] gap-y-2 gap-x-4">
        <dt class="text-sm text-muted-foreground">Email</dt>
        <dd class="m-0 break-words">
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
        </dd>

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
