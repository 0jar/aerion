<script lang="ts">
  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { calendarSources } from '$extensions/calendar/frontend/stores/calendarSources.svelte'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  let nameInput = $state('')
  let urlInput = $state('')
  let usernameInput = $state('')
  let passwordInput = $state('')
  let submitting = $state(false)
  let lastError = $state('')

  $effect(() => {
    if (!open) return
    // Reset form each time the dialog opens.
    nameInput = ''
    urlInput = ''
    usernameInput = ''
    passwordInput = ''
    lastError = ''
    submitting = false
  })

  // Register with the host's dialogGuard while open. Without this, mail's
  // global Enter/Space handler in App.svelte calls e.preventDefault() on
  // the dialog buttons. Same pattern AddContactDialog uses.
  $effect(() => {
    if (open) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  function close() {
    if (submitting) return
    open = false
    onClose?.()
  }

  function validate(): boolean {
    lastError = ''
    if (nameInput.trim() === '') {
      lastError = $_('calendar.add.fieldRequired', { values: { field: $_('calendar.add.nameLabel') } })
      return false
    }
    if (urlInput.trim() === '') {
      lastError = $_('calendar.add.fieldRequired', { values: { field: $_('calendar.add.urlLabel') } })
      return false
    }
    if (usernameInput.trim() === '') {
      lastError = $_('calendar.add.fieldRequired', { values: { field: $_('calendar.add.usernameLabel') } })
      return false
    }
    if (passwordInput === '') {
      lastError = $_('calendar.add.fieldRequired', { values: { field: $_('calendar.add.passwordLabel') } })
      return false
    }
    return true
  }

  async function submit() {
    if (!validate()) return
    submitting = true
    lastError = ''
    try {
      await calendarSources.addCalDAVSource(
        nameInput.trim(),
        urlInput.trim(),
        usernameInput.trim(),
        passwordInput,
      )
      // Find how many calendars were discovered for the new source so we
      // can show a useful success toast. The list is already loaded by
      // addCalDAVSource → load().
      const newest = calendarSources.sources.find(s => s.name === nameInput.trim())
      const count = newest ? (calendarSources.calendarsBySource[newest.id]?.length ?? 0) : 0
      toasts.success(
        $_('calendar.add.successToast', { values: { count, name: nameInput.trim() } }),
      )
      open = false
      onClose?.()
    } catch (err) {
      lastError = (err as Error)?.message ?? String(err)
      console.error('Add CalDAV source failed:', err)
    } finally {
      submitting = false
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Enter' || submitting) return
    e.preventDefault()
    submit()
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>{$_('calendar.add.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('calendar.add.description')}
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-3 mt-2">
      <div>
        <Label for="cal-add-name">{$_('calendar.add.nameLabel')}</Label>
        <Input
          id="cal-add-name"
          type="text"
          placeholder={$_('calendar.add.namePlaceholder')}
          bind:value={nameInput}
          disabled={submitting}
          onkeydown={onKeydown}
        />
      </div>
      <div>
        <Label for="cal-add-url">{$_('calendar.add.urlLabel')}</Label>
        <Input
          id="cal-add-url"
          type="text"
          placeholder={$_('calendar.add.urlPlaceholder')}
          bind:value={urlInput}
          disabled={submitting}
          onkeydown={onKeydown}
        />
        <p class="text-xs text-muted-foreground mt-1">
          {$_('calendar.add.urlHelp')}
        </p>
      </div>
      <div>
        <Label for="cal-add-username">{$_('calendar.add.usernameLabel')}</Label>
        <Input
          id="cal-add-username"
          type="text"
          bind:value={usernameInput}
          disabled={submitting}
          onkeydown={onKeydown}
        />
      </div>
      <div>
        <Label for="cal-add-password">{$_('calendar.add.passwordLabel')}</Label>
        <Input
          id="cal-add-password"
          type="password"
          bind:value={passwordInput}
          disabled={submitting}
          onkeydown={onKeydown}
        />
      </div>

      {#if lastError !== ''}
        <div class="flex items-start gap-2 p-2 bg-destructive/10 rounded text-sm min-w-0">
          <Icon icon="mdi:alert-circle" class="w-4 h-4 text-destructive shrink-0 mt-0.5" />
          <div class="flex-1 min-w-0">
            <div class="text-destructive font-medium">{$_('calendar.add.errorTitle')}</div>
            <div class="text-xs text-muted-foreground break-all max-h-24 overflow-y-auto">{lastError}</div>
            <div class="text-xs text-muted-foreground mt-1">{$_('calendar.add.errorHelp')}</div>
          </div>
        </div>
      {/if}
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={submitting}>
        {$_('calendar.common.cancel')}
      </Button>
      <Button onclick={submit} disabled={submitting}>
        {#if submitting}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
          {$_('calendar.add.submitting')}
        {:else}
          {$_('calendar.add.submit')}
        {/if}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
