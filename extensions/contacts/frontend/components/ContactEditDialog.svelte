<script lang="ts">
  // ContactEditDialog — edits a single local (sent-recipient) contact.
  // Phase 2b.1: only renames are supported (the local store has only
  // email + display_name fields; email is the primary key, not editable).
  //
  // For CardDAV / Google / Microsoft contact edits, a richer dialog with
  // multi-field support lands in 2b.2 / 2b.3 alongside the provider write
  // paths.

  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import { updateLocalContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    contact: v1.Contact | null
    onClose?: () => void
  }

  let { open = $bindable(false), contact, onClose }: Props = $props()

  let nameInput = $state('')
  let saving = $state(false)

  // Initialize the input each time the dialog opens.
  $effect(() => {
    if (open && contact) {
      nameInput = contact.name ?? ''
    }
  })

  // Register with the host's dialogGuard while open. Without this, mail's
  // global Enter/Space handler in App.svelte calls e.preventDefault() on the
  // dialog buttons (they're in a bits-ui portal, outside any pane). Same
  // pattern mail's SettingsDialog.svelte:87–92 and AccountDialog.svelte:140–141
  // use for their own dialogs — the convention is "consumer registers."
  $effect(() => {
    if (open) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  // Display the actual email (from contact.emails[0]) — NOT contact.id, which
  // is the record_id (e.g. "local-alice@example.com" for local records). The
  // record_id includes a synthetic prefix and isn't meant for display.
  // contact.id is what we pass to backend ops; email is for showing only.
  const email = $derived(contact && contact.emails && contact.emails.length > 0 ? contact.emails[0] : '')
  const recordID = $derived(contact?.id ?? '')

  function close() {
    open = false
    onClose?.()
  }

  async function save() {
    if (!recordID) return
    saving = true
    try {
      await updateLocalContact(recordID, nameInput.trim())
      toasts.success('Contact updated')
      close()
    } catch (err) {
      console.error('Failed to update local contact:', err)
      toasts.error(`Failed to update contact: ${(err as Error)?.message ?? err}`)
    } finally {
      saving = false
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>Edit contact</Dialog.Title>
      <Dialog.Description>
        Rename this contact. The email address is the unique identifier and
        cannot be changed.
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-3 mt-2">
      <div>
        <Label for="contact-edit-email">Email</Label>
        <Input id="contact-edit-email" type="text" value={email} disabled />
      </div>
      <div>
        <Label for="contact-edit-name">Display name</Label>
        <Input id="contact-edit-name" type="text" bind:value={nameInput} disabled={saving} />
      </div>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={saving}>Cancel</Button>
      <Button onclick={save} disabled={saving || !recordID}>
        {#if saving}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        Save
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
