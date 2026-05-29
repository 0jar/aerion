<script lang="ts">
  // AddContactDialog — creates a new manually-added local contact. Distinct
  // from ContactEditDialog (which derives email from contact.id and doesn't
  // expose email as an editable field). Add Contact paths in 2b.1 are
  // local-only; CardDAV / Google / Microsoft Add land in 2b.2 / 2b.3 along
  // with their respective write paths.

  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import { createLocalContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'

  interface Props {
    open: boolean
    onClose?: () => void
    onCreated?: (id: string) => void
  }

  let { open = $bindable(false), onClose, onCreated }: Props = $props()

  let emailInput = $state('')
  let nameInput = $state('')
  let saving = $state(false)
  let errors = $state<{ email?: string }>({})

  // Reset state each time the dialog opens.
  $effect(() => {
    if (!open) return
    emailInput = ''
    nameInput = ''
    errors = {}
    saving = false
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

  function validate(): boolean {
    const e = emailInput.trim().toLowerCase()
    if (e === '') {
      errors = { email: 'Email is required.' }
      return false
    }
    if (!e.includes('@') || e.indexOf('@') === e.length - 1 || e.startsWith('@')) {
      errors = { email: 'Enter a valid email address.' }
      return false
    }
    errors = {}
    return true
  }

  function close() {
    open = false
    onClose?.()
  }

  function handleSaveError(err: unknown) {
    const msg = (err as Error)?.message ?? String(err)
    if (/already exists/i.test(msg) || /UNIQUE constraint/i.test(msg)) {
      errors = { email: 'A contact with this email already exists.' }
      return
    }
    console.error('Failed to create contact:', err)
    toasts.error(`Failed to add contact: ${msg}`)
  }

  async function save() {
    if (!validate()) return
    saving = true
    try {
      const id = await createLocalContact(emailInput.trim().toLowerCase(), nameInput.trim())
      toasts.success('Contact added')
      onCreated?.(id)
      close()
    } catch (err) {
      handleSaveError(err)
    } finally {
      saving = false
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !saving) {
      e.preventDefault()
      save()
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>Add contact</Dialog.Title>
      <Dialog.Description>
        Add a new contact to your local address book. The email is the unique
        identifier and cannot be changed once saved.
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-3 mt-2">
      <div>
        <Label for="contact-add-email">Email</Label>
        <Input
          id="contact-add-email"
          type="email"
          placeholder="name@example.com"
          bind:value={emailInput}
          disabled={saving}
          onkeydown={onKeydown}
        />
        {#if errors.email}
          <p class="text-xs text-destructive mt-1">{errors.email}</p>
        {/if}
      </div>
      <div>
        <Label for="contact-add-name">Display name <span class="text-muted-foreground">(optional)</span></Label>
        <Input
          id="contact-add-name"
          type="text"
          placeholder="Jane Doe"
          bind:value={nameInput}
          disabled={saving}
          onkeydown={onKeydown}
        />
      </div>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={saving}>Cancel</Button>
      <Button onclick={save} disabled={saving}>
        {#if saving}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        Save
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
