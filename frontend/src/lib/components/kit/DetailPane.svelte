<script lang="ts">
  // DetailPane — generic right-pane shell: optional header slot, scrollable
  // body slot, and an empty-state slot rendered when `empty` is true. No
  // keyboard ownership (read-only viewing area; keys pass through to global
  // handler).

  import { type Snippet } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from 'svelte-i18n'
  import { isPaneFlashing, type FocusablePane } from '$lib/stores/keyboard.svelte'
  // Self-managed responsive (mobile) behavior — read the layout store directly
  // so consumers (extension panes) never forward responsive props. Below the
  // medium breakpoint (≤1024px) the detail pane renders as an overlay; on
  // narrow viewports a back arrow renders at the start of the header that
  // calls hideViewer to return to the list. Matches mail's ConversationViewer
  // behavior 1-for-1 via the responsive-viewer-overlay / responsive-viewer-
  // visible CSS in app.css.
  import { isResponsive, getResponsiveView, hideViewer, getLayoutMode } from '$lib/stores/layout.svelte'

  interface Props {
    /** True when there's nothing to show. Empty snippet renders when true. */
    empty?: boolean
    /** Which pane-focus slot this detail pane occupies (default 'viewer'). */
    focusSlot?: FocusablePane
    /** Header snippet (typically: title + action buttons). */
    header?: Snippet
    /** Body snippet (typically scrollable content). */
    body?: Snippet
    /** Empty-state snippet. If absent, a default placeholder renders. */
    emptyState?: Snippet
    /** Iconify identifier used in the default empty-state placeholder. */
    emptyIcon?: string
    /** Text used in the default empty-state placeholder. */
    emptyText?: string
  }

  const {
    empty = false,
    focusSlot = 'viewer',
    header,
    body,
    emptyState,
    emptyIcon = 'mdi:tray-arrow-down',
    emptyText = 'Nothing selected.',
  }: Props = $props()

  const flashing = $derived(isPaneFlashing(focusSlot))
  const overlay = $derived(isResponsive())
  const visible = $derived(getResponsiveView() === 'viewer')
  const narrow = $derived(getLayoutMode() === 'narrow')
</script>

<section class="flex-1 min-w-0 flex flex-col bg-background {flashing ? 'pane-focus-flash' : ''} {overlay ? 'responsive-viewer-overlay' : ''} {overlay && visible ? 'responsive-viewer-visible' : ''}">
  {#if empty}
    <div class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3 p-6">
      {#if emptyState}
        {@render emptyState()}
      {:else}
        <Icon icon={emptyIcon} width="48" height="48" />
        <p class="text-lg">{emptyText}</p>
      {/if}
    </div>
  {:else}
    {#if header || narrow}
      <header class="flex items-center gap-3 px-6 py-4 border-b border-border">
        {#if narrow}
          <button
            type="button"
            class="flex items-center justify-center w-8 h-8 -ml-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
            onclick={hideViewer}
            aria-label={$_('common.back')}
          >
            <Icon icon="mdi:arrow-left" class="w-5 h-5" />
          </button>
        {/if}
        {#if header}
          {@render header()}
        {/if}
      </header>
    {/if}
    <div class="flex-1 min-h-0 overflow-y-auto p-6">
      {#if body}
        {@render body()}
      {/if}
    </div>
  {/if}
</section>
