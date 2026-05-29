<!--
  ContactEditDialog — multi-field Edit dialog for local + CardDAV contacts.

  Phase 2b.2.b.2. Single scrolling form whose order matches the detail pane
  (familiar). Curated TYPE dropdowns via TypeSelect kit primitive with a
  "Custom…" override. PHOTO section at top with file picker + 256×256 auto-
  resize via Canvas (caps DB bloat at ~25KB per contact). vCard 3.0 FN
  (display name) is the only hard requirement — phone-only / address-only
  contacts are valid, matching real-world phone-imported CardDAV records.
-->
<script lang="ts">
  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import TypeSelect, {
    EMAIL_TYPES,
    PHONE_TYPES,
    ADDRESS_TYPES,
    URL_TYPES,
    IMPP_TYPES,
  } from '$lib/components/kit/TypeSelect.svelte'
  import { updateContact } from '$extensions/contacts/frontend/stores/contactsView.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import { Contacts_ResizeContactPhoto as ResizeContactPhoto } from '$wailsjs/go/app/App'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    contact: v1.Contact | null
    onClose?: () => void
  }

  let { open = $bindable(false), contact, onClose }: Props = $props()

  // Form state — mirrors ContactPatch shape but uses concrete (non-pointer)
  // values for ergonomic binding. Slices for multi-value fields. Photo is a
  // grouped object that maps onto coreapi.ContactPhoto at save time.
  type EmailRow = { email: string; type: string; isPrimary: boolean }
  type PhoneRow = { number: string; type: string; isPrimary: boolean }
  type AddressRow = {
    type: string
    street: string
    city: string
    region: string
    postcode: string
    country: string
  }
  type URLRow = { url: string; type: string }
  type IMPPRow = { handle: string; type: string }
  type PhotoState = { data: string; mediaType: string; url: string }

  let nameInput = $state('')
  let nicknameInput = $state('')
  let orgInput = $state('')
  let titleInput = $state('')
  let noteInput = $state('')
  let bdayInput = $state('')
  let emails = $state<EmailRow[]>([])
  let phones = $state<PhoneRow[]>([])
  let addresses = $state<AddressRow[]>([])
  let urls = $state<URLRow[]>([])
  let impps = $state<IMPPRow[]>([])
  let categoriesInput = $state('')
  let photo = $state<PhotoState>({ data: '', mediaType: '', url: '' })

  let saving = $state(false)
  let errors = $state<Record<string, string>>({})

  // Initialize state each time the dialog opens. Reading from `contact` here
  // (not inside `open && contact` reactive expressions in the markup) prevents
  // a flash of stale data on dialog reopen.
  $effect(() => {
    if (open && contact) {
      nameInput = contact.name ?? ''
      nicknameInput = contact.nickname ?? ''
      orgInput = contact.org ?? ''
      titleInput = contact.title ?? ''
      noteInput = contact.note ?? ''
      bdayInput = contact.bday ?? ''
      categoriesInput = (contact.categories ?? []).join(', ')

      // Emails: prefer EmailItems (carries type + isPrimary). Fall back to the
      // flat emails list when EmailItems is empty (records that haven't been
      // re-synced under 2b.2.a yet).
      if (contact.emailItems && contact.emailItems.length > 0) {
        emails = contact.emailItems.map((e) => ({
          email: e.email,
          type: e.type ?? '',
          isPrimary: e.isPrimary ?? false,
        }))
      } else if (contact.emails && contact.emails.length > 0) {
        emails = contact.emails.map((e, i) => ({ email: e, type: '', isPrimary: i === 0 }))
      } else {
        emails = []
      }

      phones = (contact.phones ?? []).map((p) => ({
        number: p.number,
        type: p.type ?? '',
        isPrimary: p.isPrimary ?? false,
      }))
      addresses = (contact.addresses ?? []).map((a) => ({
        type: a.type ?? '',
        street: a.street ?? '',
        city: a.city ?? '',
        region: a.region ?? '',
        postcode: a.postcode ?? '',
        country: a.country ?? '',
      }))
      urls = (contact.urls ?? []).map((u) => ({ url: u.url, type: u.type ?? '' }))
      impps = (contact.impps ?? []).map((i) => ({ handle: i.handle, type: i.type ?? '' }))
      photo = {
        data: contact.photoData ?? '',
        mediaType: contact.photoMediaType ?? '',
        url: contact.photoUrl ?? '',
      }
      errors = {}
    }
  })

  // DialogGuard registration — see SettingsDialog.svelte for the canonical
  // pattern. Keeps mail's global Enter/Space handler from clobbering this
  // dialog's button activation.
  $effect(() => {
    if (open) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  const recordID = $derived(contact?.id ?? '')
  const primaryEmailForAvatar = $derived(emails.find((e) => e.isPrimary)?.email ?? emails[0]?.email ?? '')
  const hasPhotoURLOnly = $derived(!photo.data && !!photo.url)

  // ============================================================================
  // Repeater helpers
  // ============================================================================

  function addEmail() {
    emails = [...emails, { email: '', type: '', isPrimary: emails.length === 0 }]
  }
  function removeEmail(i: number) {
    emails = emails.filter((_, idx) => idx !== i)
  }
  function setEmailPrimary(i: number) {
    emails = emails.map((e, idx) => ({ ...e, isPrimary: idx === i }))
  }

  function addPhone() {
    phones = [...phones, { number: '', type: '', isPrimary: phones.length === 0 }]
  }
  function removePhone(i: number) {
    phones = phones.filter((_, idx) => idx !== i)
  }
  function setPhonePrimary(i: number) {
    phones = phones.map((p, idx) => ({ ...p, isPrimary: idx === i }))
  }

  function addAddress() {
    addresses = [...addresses, { type: '', street: '', city: '', region: '', postcode: '', country: '' }]
  }
  function removeAddress(i: number) {
    addresses = addresses.filter((_, idx) => idx !== i)
  }

  function addURL() {
    urls = [...urls, { url: '', type: '' }]
  }
  function removeURL(i: number) {
    urls = urls.filter((_, idx) => idx !== i)
  }

  function addIMPP() {
    impps = [...impps, { handle: '', type: '' }]
  }
  function removeIMPP(i: number) {
    impps = impps.filter((_, idx) => idx !== i)
  }

  // ============================================================================
  // Photo picker — frontend HTML <input> picker + backend resize via Go
  // ============================================================================
  //
  // Picker pattern matches mail's insertImage() in Composer.svelte:1273-1289:
  // dynamically create <input type="file">, append to DOM (required for
  // WebKitGTK), click, handle onchange, remove. NOT a bind:this hidden
  // element — that variant has been observed to fail in some WebKitGTK
  // configurations.
  //
  // Resize happens in Go (extensions/contacts/backend/imaging) so we don't
  // ship raw multi-MB image bytes through the Wails bridge any longer than
  // necessary: frontend reads file → base64 → one IPC call → backend returns
  // 256×256 JPEG base64 (~25KB).

  function triggerPhotoPicker() {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = 'image/jpeg,image/png,image/webp,image/gif'
    input.style.display = 'none'
    document.body.appendChild(input)
    input.onchange = async (e) => {
      input.remove()
      const file = (e.target as HTMLInputElement).files?.[0]
      if (file) {
        await handlePhotoFile(file)
      }
    }
    input.click()
  }

  async function handlePhotoFile(file: File) {
    try {
      const rawBase64 = await readFileAsBase64(file)
      const resized = await ResizeContactPhoto(rawBase64)
      if (!resized?.data) {
        toasts.error($_('contacts.toast.photoFailed'))
        return
      }
      photo = { data: resized.data, mediaType: resized.mediaType, url: '' }
    } catch (err) {
      console.error('Photo resize failed:', err)
      toasts.error($_('contacts.toast.photoFailed'))
    }
  }

  function removePhoto() {
    photo = { data: '', mediaType: '', url: '' }
  }

  // Read file via FileReader → base64 (without data URL prefix). Mirrors the
  // existing helper in lib/components/composer/composerUtils.ts:93 — duplicated
  // locally rather than imported across extension boundaries so the contacts
  // extension stays self-contained (future tarball-loaded extension model).
  function readFileAsBase64(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => {
        const result = reader.result as string
        // Strip the "data:<mime>;base64," prefix.
        const base64 = result.split(',')[1]
        resolve(base64)
      }
      reader.onerror = () => reject(reader.error)
      reader.readAsDataURL(file)
    })
  }

  // ============================================================================
  // Save
  // ============================================================================

  function isValidEmail(e: string): boolean {
    const t = e.trim().toLowerCase()
    if (t === '') return false
    if (!t.includes('@') || t.indexOf('@') === t.length - 1 || t.startsWith('@')) return false
    return true
  }

  function validate(): boolean {
    const next: Record<string, string> = {}
    if (nameInput.trim() === '') {
      next.name = $_('contacts.edit.nameRequired')
    }
    // Validate non-empty email rows (empty rows are silently dropped on save).
    emails.forEach((e, i) => {
      if (e.email.trim() !== '' && !isValidEmail(e.email)) {
        next[`email-${i}`] = $_('contacts.edit.emailInvalid')
      }
    })
    errors = next
    return Object.keys(next).length === 0
  }

  async function save() {
    if (!recordID) return
    if (!validate()) return
    saving = true
    try {
      // Wails-generated `v1.ContactPatch` class has a `convertValues` method
      // we don't construct here; the runtime accepts plain objects since
      // marshaling is JSON-based. Cast through `unknown` so the call site
      // type-checks without needing the class instance.
      const patch = ({
        name: nameInput.trim(),
        nickname: nicknameInput.trim(),
        org: orgInput.trim(),
        title: titleInput.trim(),
        note: noteInput.trim(),
        bday: bdayInput.trim(),
        // Repeaters — filter empty rows so we don't waste DB writes on blanks.
        emails: emails
          .filter((e) => e.email.trim() !== '')
          .map((e) => ({ email: e.email.trim().toLowerCase(), type: e.type, isPrimary: e.isPrimary })),
        phones: phones
          .filter((p) => p.number.trim() !== '')
          .map((p) => ({ number: p.number.trim(), type: p.type, isPrimary: p.isPrimary })),
        addresses: addresses.filter(isNonEmptyAddress).map((a) => ({
          type: a.type,
          street: a.street.trim(),
          city: a.city.trim(),
          region: a.region.trim(),
          postcode: a.postcode.trim(),
          country: a.country.trim(),
        })),
        urls: urls
          .filter((u) => u.url.trim() !== '')
          .map((u) => ({ url: u.url.trim(), type: u.type })),
        impps: impps
          .filter((i) => i.handle.trim() !== '')
          .map((i) => ({ handle: i.handle.trim(), type: i.type })),
        categories: categoriesInput
          .split(',')
          .map((c) => c.trim())
          .filter((c) => c !== ''),
        // Photo: send the current state. Empty data + empty url = "remove."
        photo: {
          data: photo.data,
          mediaType: photo.mediaType,
          url: photo.url,
        },
      }) as unknown as v1.ContactPatch
      await updateContact(recordID, patch)
      toasts.success($_('contacts.toast.updated'))
      close()
    } catch (err) {
      console.error('Failed to update contact:', err)
      toasts.error($_('contacts.toast.failedUpdate'))
    } finally {
      saving = false
    }
  }

  function isNonEmptyAddress(a: AddressRow): boolean {
    return !!(a.street || a.city || a.region || a.postcode || a.country)
  }

  function close() {
    open = false
    onClose?.()
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-xl max-h-[85vh] overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>{$_('contacts.edit.title')}</Dialog.Title>
    </Dialog.Header>

    <div class="space-y-5 mt-2">
      <!-- Photo -->
      <div class="flex items-center gap-4">
        <Avatar
          email={primaryEmailForAvatar}
          name={nameInput}
          density="large"
          size={72}
          photoData={photo.data}
          photoMediaType={photo.mediaType}
        />
        <div class="flex flex-col gap-1">
          <div class="flex gap-2">
            <Button variant="outline" size="sm" onclick={triggerPhotoPicker} disabled={saving}>
              <Icon icon="mdi:image-edit-outline" class="w-4 h-4 mr-1" />
              {$_('contacts.edit.photoChange')}
            </Button>
            {#if photo.data || photo.url}
              <Button variant="ghost" size="sm" onclick={removePhoto} disabled={saving}>
                {$_('contacts.edit.photoRemove')}
              </Button>
            {/if}
          </div>
          {#if hasPhotoURLOnly}
            <span class="text-xs text-muted-foreground">{$_('contacts.edit.photoUrlOnly')}</span>
          {/if}
        </div>
      </div>

      <!-- Display name -->
      <div>
        <Label for="edit-name">{$_('contacts.edit.name')}</Label>
        <Input
          id="edit-name"
          type="text"
          bind:value={nameInput}
          disabled={saving}
          aria-invalid={errors.name ? 'true' : undefined}
        />
        {#if errors.name}
          <p class="text-xs text-destructive mt-1">{errors.name}</p>
        {/if}
      </div>

      <!-- Nickname -->
      <div>
        <Label for="edit-nickname">{$_('contacts.edit.nickname')}</Label>
        <Input id="edit-nickname" type="text" bind:value={nicknameInput} disabled={saving} />
      </div>

      <!-- Emails -->
      <div>
        <Label>{$_('contacts.edit.emails')}</Label>
        <div class="space-y-2">
          {#each emails as e, i (i)}
            <div class="flex gap-2 items-start">
              <div class="flex-1">
                <Input
                  type="email"
                  bind:value={e.email}
                  placeholder={$_('contacts.edit.emailPlaceholder')}
                  disabled={saving}
                  aria-invalid={errors[`email-${i}`] ? 'true' : undefined}
                />
                {#if errors[`email-${i}`]}
                  <p class="text-xs text-destructive mt-1">{errors[`email-${i}`]}</p>
                {/if}
              </div>
              <div class="w-32">
                <TypeSelect
                  value={e.type}
                  onValueChange={(v) => (emails[i] = { ...emails[i], type: v })}
                  options={EMAIL_TYPES}
                />
              </div>
              <label class="flex items-center gap-1 text-xs cursor-pointer pt-2" title={$_('contacts.common.primaryTooltip')}>
                <input
                  type="radio"
                  name="email-primary"
                  checked={e.isPrimary}
                  onchange={() => setEmailPrimary(i)}
                />
                <span>{$_('contacts.common.primaryLabel')}</span>
              </label>
              <Button
                variant="ghost"
                size="icon"
                onclick={() => removeEmail(i)}
                disabled={saving}
                aria-label={$_('contacts.edit.removeEmail')}
              >
                <Icon icon="mdi:close" class="w-4 h-4" />
              </Button>
            </div>
          {/each}
        </div>
        <Button variant="outline" size="sm" onclick={addEmail} disabled={saving} class="mt-2">
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('contacts.edit.addEmail')}
        </Button>
      </div>

      <!-- Phones -->
      <div>
        <Label>{$_('contacts.edit.phones')}</Label>
        <div class="space-y-2">
          {#each phones as p, i (i)}
            <div class="flex gap-2 items-center">
              <Input
                type="tel"
                bind:value={p.number}
                placeholder={$_('contacts.edit.phonePlaceholder')}
                disabled={saving}
              />
              <div class="w-32">
                <TypeSelect
                  value={p.type}
                  onValueChange={(v) => (phones[i] = { ...phones[i], type: v })}
                  options={PHONE_TYPES}
                />
              </div>
              <label class="flex items-center gap-1 text-xs cursor-pointer" title={$_('contacts.common.primaryTooltip')}>
                <input
                  type="radio"
                  name="phone-primary"
                  checked={p.isPrimary}
                  onchange={() => setPhonePrimary(i)}
                />
                <span>{$_('contacts.common.primaryLabel')}</span>
              </label>
              <Button
                variant="ghost"
                size="icon"
                onclick={() => removePhone(i)}
                disabled={saving}
                aria-label={$_('contacts.edit.removePhone')}
              >
                <Icon icon="mdi:close" class="w-4 h-4" />
              </Button>
            </div>
          {/each}
        </div>
        <Button variant="outline" size="sm" onclick={addPhone} disabled={saving} class="mt-2">
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('contacts.edit.addPhone')}
        </Button>
      </div>

      <!-- Addresses -->
      <div>
        <Label>{$_('contacts.edit.addresses')}</Label>
        <div class="space-y-3">
          {#each addresses as a, i (i)}
            <div class="border border-border rounded p-3 space-y-2">
              <div class="flex justify-between items-center">
                <div class="w-32">
                  <TypeSelect
                    value={a.type}
                    onValueChange={(v) => (addresses[i] = { ...addresses[i], type: v })}
                    options={ADDRESS_TYPES}
                  />
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  onclick={() => removeAddress(i)}
                  disabled={saving}
                  aria-label={$_('contacts.edit.removeAddress')}
                >
                  <Icon icon="mdi:close" class="w-4 h-4" />
                </Button>
              </div>
              <Input type="text" bind:value={a.street} placeholder={$_('contacts.edit.addressStreet')} disabled={saving} />
              <div class="grid grid-cols-2 gap-2">
                <Input type="text" bind:value={a.city} placeholder={$_('contacts.edit.addressCity')} disabled={saving} />
                <Input type="text" bind:value={a.region} placeholder={$_('contacts.edit.addressRegion')} disabled={saving} />
              </div>
              <div class="grid grid-cols-2 gap-2">
                <Input type="text" bind:value={a.postcode} placeholder={$_('contacts.edit.addressPostcode')} disabled={saving} />
                <Input type="text" bind:value={a.country} placeholder={$_('contacts.edit.addressCountry')} disabled={saving} />
              </div>
            </div>
          {/each}
        </div>
        <Button variant="outline" size="sm" onclick={addAddress} disabled={saving} class="mt-2">
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('contacts.edit.addAddress')}
        </Button>
      </div>

      <!-- Org / Title -->
      <div class="grid grid-cols-2 gap-3">
        <div>
          <Label for="edit-org">{$_('contacts.edit.org')}</Label>
          <Input id="edit-org" type="text" bind:value={orgInput} disabled={saving} />
        </div>
        <div>
          <Label for="edit-title">{$_('contacts.edit.titleField')}</Label>
          <Input id="edit-title" type="text" bind:value={titleInput} disabled={saving} />
        </div>
      </div>

      <!-- URLs -->
      <div>
        <Label>{$_('contacts.edit.urls')}</Label>
        <div class="space-y-2">
          {#each urls as u, i (i)}
            <div class="flex gap-2 items-center">
              <Input
                type="url"
                bind:value={u.url}
                placeholder={$_('contacts.edit.urlPlaceholder')}
                disabled={saving}
              />
              <div class="w-32">
                <TypeSelect
                  value={u.type}
                  onValueChange={(v) => (urls[i] = { ...urls[i], type: v })}
                  options={URL_TYPES}
                />
              </div>
              <Button
                variant="ghost"
                size="icon"
                onclick={() => removeURL(i)}
                disabled={saving}
                aria-label={$_('contacts.edit.removeUrl')}
              >
                <Icon icon="mdi:close" class="w-4 h-4" />
              </Button>
            </div>
          {/each}
        </div>
        <Button variant="outline" size="sm" onclick={addURL} disabled={saving} class="mt-2">
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('contacts.edit.addUrl')}
        </Button>
      </div>

      <!-- IMPPs -->
      <div>
        <Label>{$_('contacts.edit.impps')}</Label>
        <div class="space-y-2">
          {#each impps as im, i (i)}
            <div class="flex gap-2 items-center">
              <Input
                type="text"
                bind:value={im.handle}
                placeholder={$_('contacts.edit.imppPlaceholder')}
                disabled={saving}
              />
              <div class="w-32">
                <TypeSelect
                  value={im.type}
                  onValueChange={(v) => (impps[i] = { ...impps[i], type: v })}
                  options={IMPP_TYPES}
                />
              </div>
              <Button
                variant="ghost"
                size="icon"
                onclick={() => removeIMPP(i)}
                disabled={saving}
                aria-label={$_('contacts.edit.removeImpp')}
              >
                <Icon icon="mdi:close" class="w-4 h-4" />
              </Button>
            </div>
          {/each}
        </div>
        <Button variant="outline" size="sm" onclick={addIMPP} disabled={saving} class="mt-2">
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('contacts.edit.addImpp')}
        </Button>
      </div>

      <!-- Categories -->
      <div>
        <Label for="edit-categories">{$_('contacts.edit.categoriesLabel')}</Label>
        <Input
          id="edit-categories"
          type="text"
          bind:value={categoriesInput}
          placeholder={$_('contacts.edit.categoriesPlaceholder')}
          disabled={saving}
        />
      </div>

      <!-- Birthday -->
      <div>
        <Label for="edit-bday">{$_('contacts.edit.bday')}</Label>
        <Input id="edit-bday" type="date" bind:value={bdayInput} disabled={saving} />
      </div>

      <!-- Note -->
      <div>
        <Label for="edit-note">{$_('contacts.edit.note')}</Label>
        <textarea
          id="edit-note"
          bind:value={noteInput}
          disabled={saving}
          rows={3}
          class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-y"
        ></textarea>
      </div>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4 sticky bottom-0 bg-background">
      <Button variant="ghost" onclick={close} disabled={saving}>{$_('contacts.common.cancel')}</Button>
      <Button onclick={save} disabled={saving || !recordID}>
        {#if saving}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        {$_('contacts.common.save')}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
