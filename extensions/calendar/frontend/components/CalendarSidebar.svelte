<script lang="ts">
  // CalendarSidebar — left sidebar listing sources + their calendars.
  // Issue #118 pattern: one section per source, per-calendar rows with
  // checkbox + color swatch + truncated name. Bottom strip (anchored to the
  // bottom via flex-1 spacer): + Add CalDAV button, last-sync indicator,
  // settings cog.
  //
  // NOT built on kit SourceSidebar because (a) calendar's semantics are
  // multi-toggle visibility, not single-source-select, so SourceSidebar's
  // keyboard nav doesn't fit, and (b) the bottom-anchored action strip
  // needs a flex parent we control (SourceSidebar bakes its own chrome).
  // Visual shape matches the contacts sidebar (w-60, border-r, bg-muted/30).

  import { _ } from 'svelte-i18n'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import { calendarSources } from '$extensions/calendar/frontend/stores/calendarSources.svelte'
  // @ts-ignore - wailsjs bindings
  import type { backend } from '$wailsjs/go/models'

  interface Props {
    onAddSource?: () => void
    onOpenSettings?: () => void
  }

  let { onAddSource, onOpenSettings }: Props = $props()

  // Sections derived from the sources store. Empty when no sources are
  // configured yet — the bottom-strip "Add CalDAV" button is the entry point.
  const sections = $derived.by(() => {
    const out: { source: backend.Source; calendars: backend.Calendar[] }[] = []
    for (const src of calendarSources.sources) {
      const cals = calendarSources.calendarsBySource[src.id] || []
      out.push({ source: src, calendars: cals })
    }
    return out
  })

  // Friendly last-sync label for the bottom strip. Picks the most-recent
  // last_synced_at across sources; "never" if none have synced yet.
  const lastSyncedLabel = $derived.by(() => {
    let latest = 0
    for (const s of calendarSources.sources) {
      if (s.lastSyncedAt > latest) latest = s.lastSyncedAt
    }
    if (latest === 0) return $_('calendar.sidebar.lastSyncNever')
    const elapsedSec = Math.floor(Date.now() / 1000) - latest
    if (elapsedSec < 60) return $_('calendar.sidebar.lastSync', { values: { time: 'just now' } })
    if (elapsedSec < 3600) return $_('calendar.sidebar.lastSync', { values: { time: `${Math.floor(elapsedSec / 60)}m ago` } })
    if (elapsedSec < 86400) return $_('calendar.sidebar.lastSync', { values: { time: `${Math.floor(elapsedSec / 3600)}h ago` } })
    return $_('calendar.sidebar.lastSync', { values: { time: `${Math.floor(elapsedSec / 86400)}d ago` } })
  })

  function toggleVisibility(cal: backend.Calendar) {
    void calendarSources.setVisible(cal.id, !cal.visible)
  }
</script>

<aside class="w-60 flex-shrink-0 flex flex-col border-r border-border bg-muted/30">
  <h2 class="px-4 py-3 text-sm font-semibold text-foreground border-b border-border">
    {$_('calendar.sidebar.title')}
  </h2>

  <!-- Scrollable section list — takes the remaining vertical space so the
       bottom strip stays anchored at the bottom of the sidebar. -->
  <div class="flex-1 min-h-0 overflow-y-auto py-2">
    {#if sections.length === 0}
      <p class="px-4 py-3 text-xs text-muted-foreground">
        {$_('calendar.sidebar.noCalendars')}
      </p>
    {/if}

    {#each sections as section (section.source.id)}
      <div class="mb-3">
        <div class="px-4 mb-1 text-[11px] uppercase tracking-wider text-muted-foreground truncate">
          {section.source.name}
        </div>
        {#each section.calendars as cal (cal.id)}
          <div
            class="group flex items-center gap-2 mx-2 px-2 py-1.5 rounded-md cursor-pointer text-sm
                   hover:bg-muted/40 transition-colors"
            role="button"
            tabindex="0"
            onclick={() => toggleVisibility(cal)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                toggleVisibility(cal)
              }
            }}
          >
            <input
              type="checkbox"
              class="shrink-0 cursor-pointer accent-primary"
              checked={cal.visible}
              onclick={(e) => { e.stopPropagation(); toggleVisibility(cal) }}
              aria-label={cal.displayName}
            />
            <span
              class="shrink-0 inline-block w-2.5 h-2.5 rounded-full"
              style:background-color={calendarSources.colorOf(cal.id)}
              aria-hidden="true"
            ></span>
            <span class="flex-1 min-w-0 truncate text-foreground">{cal.displayName}</span>
          </div>
        {/each}
      </div>
    {/each}
  </div>

  <!-- Bottom strip, anchored at the bottom. -->
  <div class="flex flex-col gap-1 px-3 py-2 border-t border-border bg-background/40 shrink-0">
    <Button
      variant="ghost"
      size="sm"
      class="justify-start h-8"
      onclick={() => onAddSource?.()}
    >
      <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
      {$_('calendar.sidebar.addSource')}
    </Button>
    <div class="flex items-center justify-between text-xs text-muted-foreground">
      <span class="flex items-center gap-1 min-w-0 truncate">
        <Icon icon="mdi:sync" class="w-3 h-3 shrink-0" />
        <span class="truncate">{lastSyncedLabel}</span>
      </span>
      <button
        class="p-1 rounded hover:bg-muted/40 shrink-0"
        title={$_('calendar.sidebar.settings')}
        onclick={() => onOpenSettings?.()}
        type="button"
        aria-label={$_('calendar.sidebar.settings')}
      >
        <Icon icon="mdi:cog-outline" class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</aside>
