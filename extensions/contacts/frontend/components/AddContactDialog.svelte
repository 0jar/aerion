<script lang="ts">
  // AddContactDialog — creates a new contact in the user-picked source. Track B
  // (2b.2.c) added the source picker; pre-B this dialog only created into
  // the local manual store. Source dispatch happens in the backend via
  // ContactCreateInput.SourceID — local:manual for "Local", or the CardDAV
  // source UUID for CardDAV-backed sources. The local option's underlying
  // value is ALWAYS 'local:manual' regardless of which local sub-view the
  // sidebar is showing (the 'collected' kind is reserved for the sent-mail
  // collection process).
  //
  // Multi-field Add (mirroring the Edit dialog's rich shape) is out of scope
  // for B; this dialog stays at email + name. Later expansion is a separate
  // track.

  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import { contactsView, createContact, listAddressbooks } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { contactSourcesStore } from '$extensions/contacts/frontend/stores/contactSources.svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    onClose?: () => void
    onCreated?: (id: string, sourceId: string) => void
  }

  let { open = $bindable(false), onClose, onCreated }: Props = $props()

  // Local sentinel — single underlying value for the "Local" picker option.
  // 'local:manual' is the only writable local kind; 'local' (parent) and
  // 'local:collected' are filter values, not write targets.
  const LOCAL_VALUE = 'local:manual'

  // Form state.
  let sourceValue = $state<string>(LOCAL_VALUE)
  let addressbookValue = $state<string>('')
  let emailInput = $state('')
  let nameInput = $state('')
  let saving = $state(false)
  let errors = $state<{ email?: string }>({})

  // Addressbook cache for the currently-picked CardDAV source. Refreshed on
  // source change; null until first fetch completes.
  let addressbooks = $state<v1.Addressbook[]>([])
  let loadingAddressbooks = $state<boolean>(false)

  // Picker options. CardDAV sources are filtered to writable; OAuth excluded
  // until 2b.3.
  type PickerOption = { value: string; label: string }
  const sourceOptions: PickerOption[] = $derived.by(() => {
    const opts: PickerOption[] = [
      { value: LOCAL_VALUE, label: $_('contacts.add.localOption') },
    ]
    for (const s of contactSourcesStore.sources) {
      if (s.type === 'carddav' && s.writable) {
        opts.push({ value: s.id, label: s.name })
      }
    }
    return opts
  })

  function findOption(value: string): PickerOption | undefined {
    return sourceOptions.find(o => o.value === value)
  }

  // Auto-fill from the sidebar's current source when the dialog opens. Rules:
  // - "" / "local" / "local:*" → Local picker (LOCAL_VALUE, dispatches as
  //   'local:manual' regardless of which local sub-view sourced it).
  // - CardDAV UUID present in the picker → that source.
  // - CardDAV UUID NOT in the picker (non-writable / unknown) → Local fallback.
  function autoFillFromSidebar(): string {
    const sel = contactsView.selectedSourceId
    if (!sel || sel === 'local' || sel.startsWith('local:')) return LOCAL_VALUE
    const match = sourceOptions.find(o => o.value === sel)
    return match ? sel : LOCAL_VALUE
  }

  // Reset state each time the dialog opens.
  $effect(() => {
    if (!open) return
    // Make sure sources are loaded before computing the auto-fill.
    contactSourcesStore.load()
    sourceValue = autoFillFromSidebar()
    addressbookValue = ''
    emailInput = ''
    nameInput = ''
    errors = {}
    saving = false
  })

  // Fetch addressbooks whenever the user picks a CardDAV source (or the
  // dialog auto-fills to one). Local doesn't need an addressbook fetch.
  $effect(() => {
    if (!open) return
    if (sourceValue === LOCAL_VALUE) {
      addressbooks = []
      addressbookValue = ''
      return
    }
    loadingAddressbooks = true
    listAddressbooks(sourceValue)
      .then(abs => {
        addressbooks = abs
        // Pre-select the first addressbook. Single-addressbook sources still
        // get a value here so the form-state is always coherent; the dropdown
        // just isn't rendered when count === 1.
        addressbookValue = abs.length > 0 ? abs[0].id : ''
      })
      .finally(() => {
        loadingAddressbooks = false
      })
  })

  // Register with the host's dialogGuard while open. Without this, mail's
  // global Enter/Space handler in App.svelte calls e.preventDefault() on the
  // dialog buttons (they're in a bits-ui portal, outside any pane). Same
  // pattern mail's SettingsDialog and AccountDialog use for their dialogs —
  // the convention is "consumer registers."
  $effect(() => {
    if (open) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  function validate(): boolean {
    const e = emailInput.trim().toLowerCase()
    if (e === '') {
      errors = { email: $_('contacts.add.errorEmailRequired') }
      return false
    }
    if (!e.includes('@') || e.indexOf('@') === e.length - 1 || e.startsWith('@')) {
      errors = { email: $_('contacts.add.errorEmailInvalid') }
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
      errors = { email: $_('contacts.add.errorEmailExists') }
      return
    }
    console.error('Failed to create contact:', err)
    toasts.error($_('contacts.toast.failedAdd'))
  }

  async function save() {
    if (!validate()) return
    saving = true
    try {
      const input: v1.ContactCreateInput = {
        sourceId: sourceValue,
        addressbookId: sourceValue === LOCAL_VALUE ? '' : addressbookValue,
        email: emailInput.trim().toLowerCase(),
        name: nameInput.trim(),
      }
      const id = await createContact(input)
      toasts.success($_('contacts.toast.added'))
      onCreated?.(id, sourceValue)
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

  // Derived: addressbook dropdown is hidden when the source has 0 or 1
  // addressbooks (nothing meaningful to pick). 0 case typically means the
  // sources haven't loaded yet or the source genuinely has none; backend
  // surfaces a clear error on save.
  let showAddressbookPicker = $derived(
    sourceValue !== LOCAL_VALUE && addressbooks.length > 1,
  )
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>{$_('contacts.add.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('contacts.add.description')}
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-3 mt-2">
      <!-- Source picker -->
      <div>
        <Label>{$_('contacts.add.sourceLabel')}</Label>
        <Select.Root value={sourceValue} onValueChange={(v) => { sourceValue = v }} disabled={saving}>
          <Select.Trigger>
            <Select.Value placeholder={$_('contacts.add.sourcePlaceholder')}>
              {findOption(sourceValue)?.label || $_('contacts.add.sourcePlaceholder')}
            </Select.Value>
          </Select.Trigger>
          <Select.Content>
            {#each sourceOptions as opt (opt.value)}
              <Select.Item value={opt.value} label={opt.label} />
            {/each}
          </Select.Content>
        </Select.Root>
      </div>

      <!-- Addressbook sub-picker (CardDAV multi-addressbook only) -->
      {#if showAddressbookPicker}
        <div>
          <Label>{$_('contacts.add.addressbookLabel')}</Label>
          <Select.Root value={addressbookValue} onValueChange={(v) => { addressbookValue = v }} disabled={saving || loadingAddressbooks}>
            <Select.Trigger>
              <Select.Value placeholder={$_('contacts.add.addressbookPlaceholder')}>
                {addressbooks.find(a => a.id === addressbookValue)?.name || $_('contacts.add.addressbookPlaceholder')}
              </Select.Value>
            </Select.Trigger>
            <Select.Content>
              {#each addressbooks as ab (ab.id)}
                <Select.Item value={ab.id} label={ab.name} />
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
      {/if}

      <div>
        <Label for="contact-add-email">{$_('contacts.add.emailLabel')}</Label>
        <Input
          id="contact-add-email"
          type="email"
          placeholder={$_('contacts.add.emailPlaceholder')}
          bind:value={emailInput}
          disabled={saving}
          onkeydown={onKeydown}
        />
        {#if errors.email}
          <p class="text-xs text-destructive mt-1">{errors.email}</p>
        {/if}
      </div>
      <div>
        <Label for="contact-add-name">{$_('contacts.add.nameLabel')} <span class="text-muted-foreground">{$_('contacts.add.nameOptional')}</span></Label>
        <Input
          id="contact-add-name"
          type="text"
          placeholder={$_('contacts.add.namePlaceholder')}
          bind:value={nameInput}
          disabled={saving}
          onkeydown={onKeydown}
        />
      </div>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={saving}>{$_('contacts.common.cancel')}</Button>
      <Button onclick={save} disabled={saving}>
        {#if saving}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        {$_('contacts.common.save')}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
