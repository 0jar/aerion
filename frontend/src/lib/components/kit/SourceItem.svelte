<script lang="ts">
  // SourceItem — clickable row inside SourceSidebar. Rendered as a
  // non-focusable <div role="option"> for the same reason as ListRow:
  // DOM focus stays on the parent SourceSidebar container so keyboard
  // nav (J/K/Up/Down) continues working after a click.

  import Icon from '@iconify/svelte'

  interface Props {
    icon?: string
    label: string
    active?: boolean
    onclick?: (e: MouseEvent) => void
  }

  const { icon, label, active = false, onclick }: Props = $props()
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_interactive_supports_focus -->
<div
  role="option"
  aria-selected={active}
  class="flex items-center gap-2.5 w-full px-4 py-2 border-l-[3px] text-left text-foreground transition-colors cursor-pointer select-none {active
    ? 'border-l-primary bg-accent/40'
    : 'border-l-transparent hover:bg-accent/30'}"
  onclick={(e) => onclick?.(e)}
>
  {#if icon}
    <Icon {icon} width="18" height="18" />
  {/if}
  <span class="truncate">{label}</span>
</div>
