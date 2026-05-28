<script lang="ts" generics="T extends { id: string }">
  // ListPane — generic vertical list with keyboard navigation, selection, and
  // accent-bar styling. Owns its keyboard via tabindex=0 + local keydown
  // listener; stopPropagation when matched so the global App.svelte handler
  // doesn't double-fire.
  //
  // Key bindings come from frontend/src/lib/keyboard/shortcuts.ts — same
  // source mail's MessageList consumes. Rebinding j/k → other keys changes
  // shortcuts.ts only; both sides follow.
  //
  // Pane-focus store integration: when the user clicks into the list or it
  // receives DOM focus, we call setFocusedPane(focusSlot). When focusedPane
  // === focusSlot, we DOM-focus the container so subsequent keypresses route
  // here. This lets Alt+H/L cycle uniformly across mail and extension panes.

  import { onMount, type Snippet } from 'svelte'
  import { KEY } from '$lib/keyboard/shortcuts'
  import { setFocusedPane, getFocusedPane, isPaneFlashing, registerPaneNav, type FocusablePane } from '$lib/stores/keyboard.svelte'

  type Density = 'micro' | 'compact' | 'standard' | 'large'

  interface Props {
    /** Items to render. Each must have a stable `id`. */
    items: T[]
    /** Currently-selected id; null when nothing selected. */
    selectedId: string | null
    /** Density preset propagated to per-row layout (ListRow defaults pull from this). */
    density?: Density
    /** Which pane-focus slot this list occupies. */
    focusSlot?: FocusablePane
    /** ARIA label for the list container. */
    label?: string
    /** Renderer per row. Consumer composes ListRow + content. */
    row: Snippet<[T, { selected: boolean }]>
    /** Empty-state snippet shown when items.length === 0. */
    empty?: Snippet
    /** Loading snippet shown when `loading` is true. */
    loading?: boolean
    loadingSnippet?: Snippet

    onSelect: (id: string) => void
    /** Fired on Enter when an item is selected. Defaults to onSelect if absent. */
    onActivate?: (id: string) => void
    /** Fired on Space — optional multi-select hook. */
    onToggleCheck?: (id: string) => void
    /** Fired on Ctrl+A — optional bulk-select hook. */
    onSelectAll?: () => void
    /** Fired when the user invokes the focus-search global shortcut (Ctrl+S).
     *  The consumer (e.g., ContactList) typically focuses its own search input. */
    onFocusSearch?: () => void
  }

  const {
    items,
    selectedId,
    density = 'standard',
    focusSlot = 'messageList',
    label,
    row,
    empty,
    loading = false,
    loadingSnippet,
    onSelect,
    onActivate,
    onToggleCheck,
    onSelectAll,
    onFocusSearch,
  }: Props = $props()

  let containerRef = $state<HTMLDivElement | null>(null)

  // Take DOM focus when this slot becomes the focused pane.
  $effect(() => {
    if (getFocusedPane() === focusSlot && containerRef && document.activeElement !== containerRef) {
      containerRef.focus()
    }
  })

  function indexOf(id: string | null): number {
    if (id == null) return -1
    return items.findIndex(it => it.id === id)
  }

  function move(step: number) {
    if (items.length === 0) return
    const idx = indexOf(selectedId)
    const next = idx < 0
      ? (step > 0 ? 0 : items.length - 1)
      : Math.max(0, Math.min(items.length - 1, idx + step))
    onSelect(items[next].id)
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (KEY.LIST_NEXT(e)) {
      e.preventDefault()
      e.stopPropagation()
      move(1)
      return
    }
    if (KEY.LIST_PREV(e)) {
      e.preventDefault()
      e.stopPropagation()
      move(-1)
      return
    }
    if (KEY.LIST_OPEN(e)) {
      const id = selectedId
      if (!id) return
      e.preventDefault()
      e.stopPropagation()
      ;(onActivate ?? onSelect)(id)
      return
    }
    if (KEY.LIST_TOGGLE_CHECK(e)) {
      if (!onToggleCheck || !selectedId) return
      e.preventDefault()
      e.stopPropagation()
      onToggleCheck(selectedId)
      return
    }
    if (KEY.LIST_SELECT_ALL(e)) {
      if (!onSelectAll) return
      e.preventDefault()
      e.stopPropagation()
      onSelectAll()
      return
    }
  }

  function handleFocus() {
    if (getFocusedPane() !== focusSlot) {
      setFocusedPane(focusSlot)
    }
  }

  // Clicking anywhere in the list — including a row — should put DOM focus
  // on the container (the keyboard target), not on the clicked row. Rows
  // are non-focusable <div role="option"> elements (see ListRow); without
  // this, DOM focus would stay on whatever was previously focused, and
  // arrow keys would not flow back into the list.
  function handleMouseDown(_e: MouseEvent) {
    if (containerRef && document.activeElement !== containerRef) {
      containerRef.focus()
    }
  }

  // Register pane-nav so global Alt+? shortcuts can dispatch here.
  // (No mail-equivalent Alt shortcut targets messageList today, but the
  // registry is symmetric with SourceSidebar for future use.)
  onMount(() => registerPaneNav(focusSlot, {
    navigateNext: () => move(1),
    navigatePrev: () => move(-1),
    activate: () => {
      if (selectedId) (onActivate ?? onSelect)(selectedId)
    },
    focusSearch: onFocusSearch,
  }))

  const flashing = $derived(isPaneFlashing(focusSlot))
</script>

<div
  bind:this={containerRef}
  role="listbox"
  aria-label={label ?? 'List'}
  tabindex="0"
  class="flex-1 min-w-0 flex flex-col outline-none {flashing ? 'pane-focus-flash' : ''}"
  onkeydown={handleKeyDown}
  onfocus={handleFocus}
  onmousedown={handleMouseDown}
>
  <div class="flex-1 overflow-y-auto" aria-busy={loading}>
    {#if loading}
      {#if loadingSnippet}
        {@render loadingSnippet()}
      {:else}
        <p class="m-4 text-sm text-muted-foreground">Loading…</p>
      {/if}
    {:else if items.length === 0}
      {#if empty}
        {@render empty()}
      {:else}
        <p class="m-4 text-sm text-muted-foreground">No items.</p>
      {/if}
    {:else}
      {#each items as item (item.id)}
        {@render row(item, { selected: item.id === selectedId })}
      {/each}
    {/if}
  </div>
</div>

<style>
  /* density prop is propagated by the row renderer; container has no density-
     specific styles of its own. */
</style>
