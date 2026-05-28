<script lang="ts">
  // ListRow — generic horizontal row with hover/selected/density styling.
  //
  // Rendered as a non-focusable <div role="option"> rather than a <button>
  // so DOM focus stays on the parent ListPane container during keyboard
  // navigation. If rows were buttons, clicking one would move DOM focus to
  // that button, then arrow keys would leave the previous button visually
  // focused (with a residual outline) even after selection moved.

  import type { Snippet } from 'svelte'

  type Density = 'micro' | 'compact' | 'standard' | 'large'

  interface Props {
    selected?: boolean
    density?: Density
    onclick?: (e: MouseEvent) => void
    children: Snippet
  }

  const { selected = false, density = 'standard', onclick, children }: Props = $props()

  const PADDING: Record<Density, string> = {
    micro:    'px-3 py-1',
    compact:  'px-3 py-1.5',
    standard: 'px-3 py-2',
    large:    'px-3 py-3',
  }

  function handleClick(e: MouseEvent) {
    onclick?.(e)
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_interactive_supports_focus -->
<div
  role="option"
  aria-selected={selected}
  class="flex items-center gap-3 w-full {PADDING[density]} border-l-[3px] text-left transition-colors cursor-pointer select-none {selected
    ? 'border-l-primary bg-accent/40'
    : 'border-l-transparent hover:bg-accent/30'}"
  onclick={handleClick}
>
  {@render children()}
</div>
