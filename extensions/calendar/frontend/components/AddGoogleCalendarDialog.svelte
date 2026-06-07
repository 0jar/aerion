<script lang="ts">
  // AddGoogleCalendarDialog — minimal three-step picker for attaching a Google
  // calendar source to an existing Aerion mail account. Phase 2 Chunk 3.
  //
  // Flow:
  //   1. Account select: dropdown of Aerion accounts filtered to Gmail.
  //   2. Calendar fetch: Calendar_ListGoogleCalendarsForAccount(accountID). If
  //      the broker returns "additional consent required", surface a banner
  //      asking the user to grant calendar access (Chunk 6 will hook this to
  //      a proper consent flow; today we surface the error and the user
  //      retries from account settings).
  //   3. Confirm: checkbox list of writable calendars + name field. Calls
  //      Calendar_AddGoogleSource(accountID, name, selections).
  //
  // This is intentionally low-polish — Chunk 6 ships the production
  // AccountCalendarHookPanelGoogle (post-Gmail-add-flow integration).

  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { calendarSources } from '$extensions/calendar/frontend/stores/calendarSources.svelte'
  import AddCalendarDefaultsControl from './AddCalendarDefaultsControl.svelte'
  import { applyDefaultsAfterAdd } from '$extensions/calendar/frontend/lib/defaultsApply'
  // @ts-ignore - wailsjs bindings
  import { GetAccounts, Calendar_ListGoogleCalendarsForAccount, Calendar_AddGoogleSource, Calendar_GrantCalendarAccess } from '$wailsjs/go/app/App.js'
  // @ts-ignore - wailsjs bindings
  import type { account, backend } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  let accounts = $state<account.Account[]>([])
  let selectedAccountId = $state('')
  let calendars = $state<backend.GoogleCalendarChoice[]>([])
  let selectedIds = $state<Set<string>>(new Set())
  let sourceName = $state('')
  let providerDefaultTempId = $state('')
  let globalDefaultRef = $state('')
  let loadingAccounts = $state(false)
  let loadingCalendars = $state(false)
  let submitting = $state(false)
  let granting = $state(false)
  let errorMessage = $state('')
  let needsConsent = $state(false)

  const googleAccounts = $derived(
    accounts.filter((a) => (a.imapHost || '').includes('gmail.com') || (a.imapHost || '').includes('googlemail.com')),
  )

  $effect(() => {
    if (!open) return
    dialogGuardOpen()
    return () => dialogGuardClose()
  })

  $effect(() => {
    if (!open) return
    resetState()
    void loadAccounts()
  })

  function resetState() {
    selectedAccountId = ''
    calendars = []
    selectedIds = new Set()
    sourceName = ''
    errorMessage = ''
    needsConsent = false
    providerDefaultTempId = ''
    globalDefaultRef = ''
  }

  async function loadAccounts() {
    loadingAccounts = true
    try {
      const list = await GetAccounts()
      accounts = list || []
      if (googleAccounts.length === 1) {
        selectedAccountId = googleAccounts[0].id
        void onAccountChange()
      }
    } catch (err) {
      errorMessage = String(err)
    } finally {
      loadingAccounts = false
    }
  }

  async function onAccountChange() {
    if (!selectedAccountId) return
    errorMessage = ''
    needsConsent = false
    calendars = []
    selectedIds = new Set()
    sourceName = ''
    loadingCalendars = true
    try {
      const list = await Calendar_ListGoogleCalendarsForAccount(selectedAccountId)
      calendars = list || []
      const acct = accounts.find((a) => a.id === selectedAccountId)
      sourceName = acct?.email ? `Google: ${acct.email}` : 'Google Calendar'
    } catch (err) {
      const msg = String(err)
      needsConsent = msg.includes('additional consent required')
      if (!needsConsent) {
        errorMessage = msg
      }
    } finally {
      loadingCalendars = false
    }
  }

  // grantAccess runs the incremental-consent OAuth flow to add Calendar
  // scope to the selected Gmail account, then re-runs the calendar list
  // fetch so the picker populates inline without forcing the user to
  // dismiss + re-open the dialog.
  async function grantAccess() {
    if (!selectedAccountId || granting) return
    const acct = accounts.find((a) => a.id === selectedAccountId)
    if (!acct) return
    granting = true
    errorMessage = ''
    try {
      await Calendar_GrantCalendarAccess('google', selectedAccountId, acct.email)
      // Consent granted; re-fetch the calendar list.
      needsConsent = false
      await onAccountChange()
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      errorMessage = $_('calendar.settings.addGoogleConsentFailed', { values: { message: msg } })
    } finally {
      granting = false
    }
  }

  function toggleCalendar(id: string) {
    const next = new Set(selectedIds)
    if (next.has(id)) {
      next.delete(id)
      selectedIds = next
      return
    }
    next.add(id)
    selectedIds = next
  }

  // toggleSelectAll flips between "all calendars selected" and "none". Read-only
  // calendars are included — picker semantics match the backend, which stores
  // per-calendar writability so a read-only subscription works fine.
  function toggleSelectAll() {
    if (selectedIds.size === calendars.length) {
      selectedIds = new Set()
      return
    }
    selectedIds = new Set(calendars.map((c) => c.id))
  }

  const allSelected = $derived(calendars.length > 0 && selectedIds.size === calendars.length)
  const someSelected = $derived(selectedIds.size > 0 && selectedIds.size < calendars.length)
  let selectAllEl = $state<HTMLInputElement | null>(null)
  $effect(() => {
    if (selectAllEl) selectAllEl.indeterminate = someSelected
  })

  async function handleSave() {
    if (submitting) return
    if (!selectedAccountId) {
      errorMessage = $_('calendar.settings.addGoogleErrorAccount')
      return
    }
    if (selectedIds.size === 0) {
      errorMessage = $_('calendar.settings.addGoogleErrorPick')
      return
    }
    if (!sourceName.trim()) {
      errorMessage = $_('calendar.settings.addGoogleErrorName')
      return
    }
    submitting = true
    errorMessage = ''
    try {
      const selections = calendars
        .filter((c) => selectedIds.has(c.id))
        .map((c) => ({ id: c.id, displayName: c.summary, color: '', writable: c.writable }))
      const newSourceID = await Calendar_AddGoogleSource(selectedAccountId, sourceName.trim(), selections)
      // Reload the sources store so the newly-persisted calendars have
      // real backend IDs we can attach defaults to (provider uses Google's
      // calendar id as the URL column; we match by URL → real id).
      await calendarSources.load()
      const newCals = calendarSources.calendarsBySource[newSourceID] || []
      const added = selections.map(s => {
        const match = newCals.find(c => c.url === s.id)
        return {
          id: match?.id ?? '',
          tempId: s.id,
          writable: s.writable,
        }
      }).filter(a => a.id !== '')
      applyDefaultsAfterAdd({
        sourceId: newSourceID,
        added,
        providerDefaultTempId,
        globalDefaultRef,
      })
      toasts.success($_('calendar.settings.addGoogleToastSuccess'))
      close()
    } catch (err) {
      errorMessage = $_('calendar.settings.addGoogleToastError', { values: { message: String(err) } })
    } finally {
      submitting = false
    }
  }

  function close() {
    open = false
    onClose?.()
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>{$_('calendar.settings.addGoogleTitle')}</Dialog.Title>
      <Dialog.Description>{$_('calendar.settings.addGoogleDescription')}</Dialog.Description>
    </Dialog.Header>

    <div class="space-y-4 py-2">
      <!-- Account picker -->
      <div class="space-y-1">
        <Label>{$_('calendar.settings.addGoogleAccountLabel')}</Label>
        {#if loadingAccounts}
          <p class="text-xs text-muted-foreground">{$_('calendar.common.loading')}</p>
        {/if}
        {#if !loadingAccounts && googleAccounts.length === 0}
          <p class="text-xs text-muted-foreground">{$_('calendar.settings.addGoogleNoAccounts')}</p>
        {/if}
        {#if !loadingAccounts && googleAccounts.length > 0}
          <select
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            bind:value={selectedAccountId}
            onchange={() => void onAccountChange()}
          >
            <option value="">{$_('calendar.settings.addGooglePickAccount')}</option>
            {#each googleAccounts as acct (acct.id)}
              <option value={acct.id}>{acct.email}</option>
            {/each}
          </select>
        {/if}
      </div>

      {#if needsConsent}
        <div class="rounded-md border border-yellow-400/40 bg-yellow-400/10 p-3 text-xs text-yellow-700 dark:text-yellow-300 space-y-2">
          <div>{$_('calendar.settings.addGoogleConsentNeeded')}</div>
          <Button size="sm" variant="outline" onclick={grantAccess} disabled={granting}>
            {#if granting}{$_('calendar.settings.addGoogleGranting')}{:else}{$_('calendar.settings.addGoogleGrantButton')}{/if}
          </Button>
        </div>
      {/if}

      <!-- Calendar list -->
      {#if loadingCalendars}
        <p class="text-xs text-muted-foreground">{$_('calendar.settings.addGoogleLoadingCalendars')}</p>
      {/if}

      {#if !loadingCalendars && calendars.length > 0}
        <div class="space-y-1">
          <Label>{$_('calendar.settings.addGoogleCalendarsLabel')}</Label>
          <div class="max-h-48 overflow-y-auto rounded-md border border-border">
            <label
              class="flex items-center gap-2 px-3 py-2 text-sm border-b border-border bg-muted/30 cursor-pointer hover:bg-muted/50"
            >
              <input
                type="checkbox"
                bind:this={selectAllEl}
                checked={allSelected}
                onchange={toggleSelectAll}
              />
              <span class="font-medium">{$_('calendar.settings.addGoogleSelectAll')}</span>
            </label>
            {#each calendars as cal (cal.id)}
              <div
                class="flex items-center gap-2 px-3 py-2 text-sm border-b border-border last:border-b-0
                       hover:bg-muted/40"
                title={!cal.writable ? $_('calendar.settings.addGoogleReadOnly') : ''}
              >
                <label class="flex items-center gap-2 flex-1 cursor-pointer min-w-0">
                  <input
                    type="checkbox"
                    checked={selectedIds.has(cal.id)}
                    onchange={() => toggleCalendar(cal.id)}
                  />
                  <span class="truncate flex-1">{cal.summary}</span>
                  {#if cal.primary}
                    <span class="text-xs text-muted-foreground">{$_('calendar.settings.addGooglePrimary')}</span>
                  {/if}
                  {#if !cal.writable}
                    <span class="text-xs text-muted-foreground">{$_('calendar.settings.addGoogleReadOnlyBadge')}</span>
                  {/if}
                </label>
                <label class="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
                  <input
                    type="radio"
                    name="google-provider-default"
                    class="accent-primary"
                    checked={providerDefaultTempId === cal.id}
                    disabled={!cal.writable || !selectedIds.has(cal.id)}
                    onchange={() => { providerDefaultTempId = cal.id }}
                  />
                  {$_('calendar.add.defaultColumnHeader')}
                </label>
              </div>
            {/each}
          </div>
        </div>

        <!-- Source name -->
        <div class="space-y-1">
          <Label for="source-name">{$_('calendar.settings.addGoogleSourceNameLabel')}</Label>
          <Input id="source-name" bind:value={sourceName} />
        </div>

        <AddCalendarDefaultsControl
          mode="multi"
          sourceId={''}
          providerLabel={sourceName || $_('calendar.add.providerLabelGoogle')}
          candidates={calendars
            .filter(c => selectedIds.has(c.id))
            .map(c => ({ tempId: c.id, displayName: c.summary, writable: c.writable }))}
          bind:providerDefaultTempId
          bind:globalDefaultRef
        />
      {/if}

      {#if errorMessage}
        <p class="text-xs text-destructive">{errorMessage}</p>
      {/if}
    </div>

    <Dialog.Footer>
      <Button variant="ghost" onclick={close}>{$_('calendar.common.cancel')}</Button>
      <Button onclick={handleSave} disabled={submitting || calendars.length === 0 || selectedIds.size === 0}>
        {submitting ? $_('calendar.common.saving') : $_('calendar.common.save')}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
