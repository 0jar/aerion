<script lang="ts">
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  // @ts-ignore - wailsjs path
  import { folder } from '../../../../wailsjs/go/models'
  import { _ } from '$lib/i18n'

  interface Props {
    open: boolean
    title: string
    foldersLoading: boolean
    specialFolders: folder.Folder[]
    customFolders: folder.Folder[]
    onSelect: (folderId: string, folderName: string) => void
  }

  let {
    open = $bindable(false),
    title,
    foldersLoading,
    specialFolders,
    customFolders,
    onSelect,
  }: Props = $props()

  let focusedIndex = $state(-1)
  let active = $state(false)
  let listEl: HTMLDivElement | undefined = $state()

  const allFolders = $derived([...specialFolders, ...customFolders])

  // Reset focus index when dialog opens/closes, with setTimeout guard to prevent
  // the Enter keydown that opened the dialog from immediately selecting a folder.
  // setTimeout(0) schedules in the next macrotask, after all event bubbling completes.
  // tick() (microtask) can resolve during the same event phase — not robust enough.
  $effect(() => {
    if (!open) {
      active = false
      return
    }
    focusedIndex = allFolders.length > 0 ? 0 : -1
    active = false
    const timer = setTimeout(() => { active = true }, 0)
    return () => clearTimeout(timer)
  })

  // Scroll focused item into view
  $effect(() => {
    if (focusedIndex < 0 || !listEl) return
    const buttons = listEl.querySelectorAll('button')
    buttons[focusedIndex]?.scrollIntoView({ block: 'nearest' })
  })

  const folderIcons: Record<string, string> = {
    inbox: 'mdi:inbox',
    sent: 'mdi:send',
    drafts: 'mdi:file-document-edit-outline',
    trash: 'mdi:delete-outline',
    archive: 'mdi:archive-outline',
    spam: 'mdi:alert-octagon-outline',
    all: 'mdi:email-multiple-outline',
    folder: 'mdi:folder-outline',
  }

  function getFolderIcon(type: string): string {
    return folderIcons[type] || folderIcons.folder
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!active || allFolders.length === 0) return

    switch (e.key) {
      case 'ArrowDown':
      case 'j':
        e.preventDefault()
        e.stopPropagation()
        focusedIndex = (focusedIndex + 1) % allFolders.length
        break
      case 'ArrowUp':
      case 'k':
        e.preventDefault()
        e.stopPropagation()
        focusedIndex = (focusedIndex - 1 + allFolders.length) % allFolders.length
        break
      case 'Enter':
        e.preventDefault()
        e.stopPropagation()
        if (focusedIndex >= 0 && focusedIndex < allFolders.length) {
          const f = allFolders[focusedIndex]
          onSelect(f.id, f.name)
        }
        break
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<Dialog.Root bind:open>
  <Dialog.Content class="max-w-sm">
    <Dialog.Header>
      <Dialog.Title>{title}</Dialog.Title>
    </Dialog.Header>

    <div
      class="border border-border rounded-md divide-y divide-border max-h-64 overflow-y-auto"
      bind:this={listEl}
      role="listbox"
    >
      {#if foldersLoading}
        <div class="flex items-center gap-2 p-3 text-muted-foreground text-sm">
          <Icon icon="mdi:loading" class="h-4 w-4 animate-spin" />
          {$_('common.loading')}
        </div>
      {:else if allFolders.length === 0}
        <div class="p-3 text-sm text-muted-foreground">
          {$_('contextMenu.noFoldersAvailable')}
        </div>
      {:else}
        {#each allFolders as f, i (f.id)}
          <button
            type="button"
            role="option"
            aria-selected={i === focusedIndex}
            class="w-full flex items-center gap-3 p-3 text-left text-sm hover:bg-muted/50 transition-colors {i === focusedIndex ? 'bg-muted/50' : ''}"
            onclick={() => onSelect(f.id, f.name)}
          >
            <Icon icon={getFolderIcon(f.type)} class="h-4 w-4 shrink-0" />
            {f.name}
          </button>
        {/each}
      {/if}
    </div>

    <Dialog.Footer>
      <Button variant="destructive" onclick={() => (open = false)}>
        {$_('common.cancel')}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
