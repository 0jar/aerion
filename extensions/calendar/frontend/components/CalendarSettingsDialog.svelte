<script lang="ts">
  // CalendarSettingsDialog — Calendar extension's settings.
  //
  // Two sections:
  //   1. Sources — per-source row with sync-interval picker, Sync Now,
  //      Delete, last-sync info. Add CalDAV button at the bottom.
  //   2. Alarms — informational copy (per-source toggles deferred).

  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import ConfirmDialog from '$lib/components/kit/ConfirmDialog.svelte'
  import ColorPicker from '$lib/components/kit/ColorPicker.svelte'
  import Icon from '@iconify/svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { calendarSources } from '$extensions/calendar/frontend/stores/calendarSources.svelte'
  import { calendarSettings } from '$extensions/calendar/frontend/stores/calendarSettings.svelte'
  import AddCalDAVSourceDialog from './AddCalDAVSourceDialog.svelte'
  // @ts-ignore - wailsjs bindings
  import { Calendar_SetSyncInterval } from '$wailsjs/go/app/App.js'
  // @ts-ignore - wailsjs bindings
  import type { backend } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  // Add-source dialog opens from inside this one.
  let showAddSource = $state(false)

  // Per-row state — pending delete + per-source spinner for Sync Now.
  let deleteTarget = $state<backend.Source | null>(null)
  let deleting = $state(false)
  let syncingSourceID = $state<string | null>(null)

  // Sync interval options (mirrors api.go's validSyncIntervals).
  const INTERVAL_VALUES = [5, 15, 30, 60, 120, 240, 720]

  function formatIntervalLabel(n: number): string {
    if (n < 60) return $_('calendar.settings.intervalMinutes', { values: { n } })
    return $_('calendar.settings.intervalHours', { values: { n: n / 60 } })
  }

  // Relative last-sync time. Uses chosen tz only for label fidelity; the
  // relative-time logic is tz-agnostic.
  function lastSyncLabel(src: backend.Source): string {
    if (!src.lastSyncedAt || src.lastSyncedAt === 0) {
      return $_('calendar.settings.lastSyncNever')
    }
    const elapsed = Math.floor(Date.now() / 1000) - src.lastSyncedAt
    let when: string
    if (elapsed < 60) when = 'just now'
    else if (elapsed < 3600) when = `${Math.floor(elapsed / 60)}m`
    else if (elapsed < 86400) when = `${Math.floor(elapsed / 3600)}h`
    else when = `${Math.floor(elapsed / 86400)}d`
    return $_('calendar.settings.lastSync', { values: { time: when } })
  }

  function calendarCountLabel(src: backend.Source): string {
    const n = calendarSources.calendarsBySource[src.id]?.length ?? 0
    if (n === 1) return $_('calendar.settings.calendarCountOne')
    return $_('calendar.settings.calendarCount', { values: { count: n } })
  }

  async function handleIntervalChange(source: backend.Source, value: string) {
    const minutes = Number(value)
    if (!Number.isFinite(minutes) || minutes <= 0) return
    try {
      await Calendar_SetSyncInterval(source.id, minutes)
      toasts.success($_('calendar.settings.intervalToastSuccess', { values: { name: source.name } }))
      void calendarSources.load()
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      toasts.error($_('calendar.settings.intervalToastError', { values: { message: msg } }))
    }
  }

  async function handleSyncNow(source: backend.Source) {
    if (syncingSourceID !== null) return
    syncingSourceID = source.id
    try {
      await calendarSources.syncSource(source.id)
    } finally {
      syncingSourceID = null
    }
  }

  async function handleConfirmDelete() {
    if (!deleteTarget) return
    deleting = true
    try {
      await calendarSources.deleteSource(deleteTarget.id)
      toasts.success($_('calendar.toast.sourceDeleted', { values: { name: deleteTarget.name } }))
      deleteTarget = null
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      toasts.error(msg)
    } finally {
      deleting = false
    }
  }

  function close() {
    open = false
    onClose?.()
  }

  $effect(() => {
    if (open) {
      dialogGuardOpen()
      void calendarSources.load()
      return () => dialogGuardClose()
    }
  })

  // The currently-bound interval per source is a derived map so each
  // Select.Root has a stable bound value.
  function currentInterval(src: backend.Source): string {
    const m = src.syncIntervalMin ?? 15
    if (!INTERVAL_VALUES.includes(m)) return '15'
    return String(m)
  }

  // Reading $locale at template time below already binds reactivity for the
  // formatter calls; no local derived needed.
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-2xl">
    <Dialog.Header>
      <Dialog.Title>{$_('calendar.settings.title')}</Dialog.Title>
    </Dialog.Header>

    <div class="space-y-6 mt-2 max-h-[60vh] overflow-y-auto pr-1">
      <!-- Sources section -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-foreground">
          {$_('calendar.settings.sourcesSection')}
        </h3>

        {#if calendarSources.sources.length === 0}
          <p class="text-sm text-muted-foreground py-3">
            {$_('calendar.settings.noSources')}
          </p>
        {/if}

        {#each calendarSources.sources as src (src.id)}
          <div class="flex flex-col gap-2 p-3 border border-border rounded-md">
            <!-- Source header row -->
            <div class="flex items-start justify-between gap-3">
              <div class="flex-1 min-w-0">
                <div class="text-sm font-medium text-foreground truncate">
                  {src.name}
                </div>
                <div class="text-xs text-muted-foreground mt-0.5">
                  {calendarCountLabel(src)} · {lastSyncLabel(src)}
                </div>
              </div>

              <div class="flex items-center gap-2 shrink-0">
                <Select.Root
                  value={currentInterval(src)}
                  onValueChange={(v) => { if (v) void handleIntervalChange(src, v) }}
                >
                  <Select.Trigger class="h-8 w-32 text-xs">
                    {formatIntervalLabel(Number(currentInterval(src)))}
                  </Select.Trigger>
                  <Select.Content>
                    {#each INTERVAL_VALUES as v (v)}
                      <Select.Item value={String(v)} label={formatIntervalLabel(v)} />
                    {/each}
                  </Select.Content>
                </Select.Root>

                <Button
                  variant="outline"
                  size="sm"
                  onclick={() => handleSyncNow(src)}
                  disabled={syncingSourceID !== null}
                  class="h-8"
                >
                  {#if syncingSourceID === src.id}
                    <Icon icon="mdi:loading" class="w-3.5 h-3.5 animate-spin" />
                  {:else}
                    <Icon icon="mdi:sync" class="w-3.5 h-3.5" />
                  {/if}
                  <span class="ml-1">{$_('calendar.settings.syncNow')}</span>
                </Button>

                <Button
                  variant="ghost"
                  size="sm"
                  class="h-8 text-destructive hover:text-destructive"
                  onclick={() => { deleteTarget = src }}
                >
                  <Icon icon="mdi:delete-outline" class="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>

            <!-- Per-calendar color rows. Color is the calendar's actual
                 rendered hue (stored hex if set, else the deterministic
                 HSL→hex fallback) — clicking opens the palette + hex input
                 popover. -->
            {#each (calendarSources.calendarsBySource[src.id] ?? []) as cal (cal.id)}
              <div class="flex items-center gap-3 pl-2">
                <ColorPicker
                  value={calendarSources.colorOfHex(cal.id)}
                  onchange={(hex) => { void calendarSources.setColor(cal.id, hex) }}
                />
                <span class="flex-1 min-w-0 truncate text-sm text-foreground">
                  {cal.displayName}
                </span>
              </div>
            {/each}
          </div>
        {/each}

        <Button
          variant="outline"
          size="sm"
          class="mt-2"
          onclick={() => { showAddSource = true }}
        >
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('calendar.sidebar.addSource')}
        </Button>
      </section>

      <!-- Alarms section (informational) -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-foreground">
          {$_('calendar.settings.alarmsSection')}
        </h3>
        <p class="text-xs text-muted-foreground">
          {$_('calendar.settings.alarmsHelp')}
        </p>
      </section>

      <!-- Display timezone — read-only hint; actual picker is in ViewSwitcher.
           This section is a discoverability nudge so users know where to find
           the setting; no UI duplication. -->
      <section class="space-y-1">
        <h3 class="text-sm font-semibold text-foreground">
          {$_('calendar.tzSelector.tooltip')}
        </h3>
        <p class="text-xs text-muted-foreground">
          {$_('calendar.viewSwitcher.tzLabel', { values: { tz: calendarSettings.effectiveTimezone } })}
        </p>
      </section>
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close}>{$_('calendar.common.close')}</Button>
    </div>
  </Dialog.Content>
</Dialog.Root>

<AddCalDAVSourceDialog bind:open={showAddSource} onClose={() => { void calendarSources.load() }} />

<ConfirmDialog
  open={deleteTarget !== null}
  title={deleteTarget ? $_('calendar.settings.deleteConfirmTitle', { values: { name: deleteTarget.name } }) : ''}
  description={$_('calendar.settings.deleteConfirmDescription')}
  confirmLabel={$_('calendar.common.delete')}
  cancelLabel={$_('calendar.common.cancel')}
  variant="destructive"
  loading={deleting}
  onConfirm={handleConfirmDelete}
  onCancel={() => { deleteTarget = null }}
/>
