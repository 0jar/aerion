<script lang="ts">
  // ContactsSettingsDialog — the Contacts extension's own settings dialog.
  // Holds the extension's OAuth Credentials section (per-extension slots:
  // google-contacts, microsoft-contacts) and any other contacts-specific
  // settings as they accumulate.
  //
  // Opens via:
  //  (1) Settings → Extensions → Edit button on the Contacts row
  //  (2) ContactsPane's auto-detect on mount when a writable source lacks
  //      the corresponding extension OAuth creds (future, 2b.3)
  //
  // Both entry paths share this single dialog component.

  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import OAuthCredsSlotEditor from '$lib/components/kit/OAuthCredsSlotEditor.svelte'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  function handleClose() {
    open = false
    onClose?.()
  }

  // Slots the user can copy creds FROM (other slots that might already hold
  // verified credentials). Pre-populated with mail slots since the common
  // workflow is "I have a verified Google project for mail; reuse that
  // verified project's creds for contacts write too" — the user pastes the
  // same Client ID + Secret into google-contacts. Copy-from shortcut handles
  // it server-side without exposing values.
  const copyFromOptions = $derived([
    { configID: 'google-mail', label: $_('contacts.settings.copyFromGoogle') },
    { configID: 'microsoft-mail', label: $_('contacts.settings.copyFromMicrosoft') },
  ])
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) handleClose() }}>
  <Dialog.Content class="max-w-2xl">
    <Dialog.Header>
      <Dialog.Title>{$_('contacts.settings.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('contacts.settings.description')}
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-4 mt-2 max-h-[60vh] overflow-y-auto pr-1">
      <section>
        <h3 class="text-sm font-semibold text-foreground mb-2">{$_('contacts.settings.oauthHeading')}</h3>
        <p class="text-xs text-muted-foreground mb-3">
          {$_('contacts.settings.oauthDescription')}
        </p>

        <div class="space-y-3">
          <OAuthCredsSlotEditor
            configID="google-contacts"
            label={$_('contacts.settings.googleLabel')}
            secretRequired={true}
            {copyFromOptions}
          />
          <OAuthCredsSlotEditor
            configID="microsoft-contacts"
            label={$_('contacts.settings.microsoftLabel')}
            secretRequired={false}
            {copyFromOptions}
          />
        </div>
      </section>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={handleClose}>{$_('contacts.settings.close')}</Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
