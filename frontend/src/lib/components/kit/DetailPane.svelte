<script lang="ts">
  // DetailPane — generic right-pane shell: optional header slot, scrollable
  // body slot, and an empty-state slot rendered when `empty` is true. No
  // keyboard ownership (read-only viewing area; keys pass through to global
  // handler).

  import { type Snippet } from 'svelte'
  import Icon from '@iconify/svelte'
  import { isPaneFlashing, type FocusablePane } from '$lib/stores/keyboard.svelte'

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
</script>

<section class="flex-1 min-w-0 flex flex-col bg-background {flashing ? 'pane-focus-flash' : ''}">
  {#if empty}
    <div class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3 p-6">
      {#if emptyState}
        {@render emptyState()}
      {:else}
        <Icon icon={emptyIcon} width="48" height="48" />
        <p>{emptyText}</p>
      {/if}
    </div>
  {:else}
    {#if header}
      <header class="flex items-center gap-3 px-6 py-4 border-b border-border">
        {@render header()}
      </header>
    {/if}
    <div class="flex-1 overflow-y-auto p-6">
      {#if body}
        {@render body()}
      {/if}
    </div>
  {/if}
</section>
