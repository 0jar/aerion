<script lang="ts" generics="T extends { id: string }">
  // SourceSidebar — sectioned left sidebar primitive for extensions.
  // Owns keyboard navigation (Up/Down/J/K within sidebar) via tabindex+focus.
  // Selection state managed by consumer via selectedId + onSelect.
  //
  // Pane-focus store integration mirrors ListPane: registers focusSlot, takes
  // DOM focus when the slot matches so Alt+H/L cycling routes here.

  import { type Snippet, onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from 'svelte-i18n'
  import { KEY } from '$lib/keyboard/shortcuts'
  import { setFocusedPane, getFocusedPane, isPaneFlashing, registerPaneNav, type FocusablePane } from '$lib/stores/keyboard.svelte'
  // Responsive (mobile) behavior is self-managed: the sidebar reads the
  // layout store directly so consumers never need to forward responsive
  // props. Below 768px we apply the overlay classes (from app.css), inject
  // a back button at the top, and auto-dismiss the sidebar after a source
  // is picked — mirroring mail's narrow-mode behavior 1-for-1.
  import { getLayoutMode, getResponsiveView, hideSidebar } from '$lib/stores/layout.svelte'

  type SourceSection<U extends { id: string }> = {
    heading?: string
    items: U[]
  }

  interface Props {
    title?: string
    sections: SourceSection<T>[]
    selectedId: string | null
    focusSlot?: FocusablePane
    label?: string
    item: Snippet<[T, { active: boolean }]>
    header?: Snippet
    sectionEmpty?: Snippet<[SourceSection<T>]>
    onSelect: (id: string) => void
  }

  const {
    title,
    sections,
    selectedId,
    focusSlot = 'sidebar',
    label,
    item,
    header,
    sectionEmpty,
    onSelect,
  }: Props = $props()

  let containerRef = $state<HTMLElement | null>(null)

  const allItems = $derived(sections.flatMap(s => s.items))

  // Responsive derived state — read directly from the layout store. No
  // consumer-supplied props. Mouse clicks bypass this component's onSelect
  // (rows wire their own onclick to the consumer's callback), so the auto-
  // dismiss behavior on source-pick lives in the consumer's selectSource
  // action, not here — same pattern mail uses with App.svelte's folder pick.
  const narrow = $derived(getLayoutMode() === 'narrow')
  const overlayVisible = $derived(narrow && getResponsiveView() === 'sidebar')

  $effect(() => {
    if (getFocusedPane() === focusSlot && containerRef && document.activeElement !== containerRef) {
      containerRef.focus()
    }
  })

  function indexOf(id: string | null): number {
    if (id == null) return -1
    return allItems.findIndex(it => it.id === id)
  }

  function move(step: number) {
    if (allItems.length === 0) return
    const idx = indexOf(selectedId)
    const next = idx < 0
      ? (step > 0 ? 0 : allItems.length - 1)
      : Math.max(0, Math.min(allItems.length - 1, idx + step))
    onSelect(allItems[next].id)
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
      if (!selectedId) return
      e.preventDefault()
      e.stopPropagation()
      onSelect(selectedId)
      return
    }
  }

  function handleFocus() {
    if (getFocusedPane() !== focusSlot) {
      setFocusedPane(focusSlot)
    }
  }

  function handleMouseDown(_e: MouseEvent) {
    if (containerRef && document.activeElement !== containerRef) {
      containerRef.focus()
    }
  }

  // Register so Alt+J/K dispatched from the global handler routes here.
  onMount(() => registerPaneNav(focusSlot, {
    navigateNext: () => move(1),
    navigatePrev: () => move(-1),
    activate: () => { if (selectedId) onSelect(selectedId) },
  }))

  const flashing = $derived(isPaneFlashing(focusSlot))
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  bind:this={containerRef}
  role="navigation"
  aria-label={label ?? title ?? 'Sources'}
  tabindex="0"
  class="w-60 flex-shrink-0 flex flex-col py-3 border-r border-border overflow-y-auto outline-none {narrow ? 'bg-background' : 'bg-muted/30'} {flashing ? 'pane-focus-flash' : ''} {narrow ? 'responsive-sidebar-overlay' : ''} {overlayVisible ? 'responsive-sidebar-visible' : ''}"
  onkeydown={handleKeyDown}
  onfocus={handleFocus}
  onmousedown={handleMouseDown}
>
  {#if narrow}
    <button
      type="button"
      class="flex items-center gap-2 px-4 py-2 mb-2 text-sm text-muted-foreground hover:text-foreground"
      onclick={hideSidebar}
      aria-label={$_('common.back')}
    >
      <Icon icon="mdi:arrow-left" class="w-4 h-4" />
      <span>{$_('common.back')}</span>
    </button>
  {/if}

  {#if title}
    <h2 class="px-4 mb-3 text-lg font-semibold text-foreground">{title}</h2>
  {/if}

  {#if header}
    {@render header()}
  {/if}

  {#each sections as section, sIdx (sIdx)}
    {#if section.heading}
      <div class="mx-4 mt-3 mb-1 text-[11px] uppercase tracking-wider text-muted-foreground">
        {section.heading}
      </div>
    {/if}

    {#if section.items.length === 0}
      {#if sectionEmpty}
        {@render sectionEmpty(section)}
      {/if}
    {:else}
      {#each section.items as it (it.id)}
        {@render item(it, { active: it.id === selectedId })}
      {/each}
    {/if}
  {/each}
</div>
