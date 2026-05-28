<script lang="ts">
  import { onMount } from 'svelte'
  import SourceSidebar from '$lib/components/kit/SourceSidebar.svelte'
  import SourceItem from '$lib/components/kit/SourceItem.svelte'
  import { contactSourcesStore } from '$lib/stores/contactSources.svelte'
  import { contactsView, selectSource } from '$extensions/contacts/frontend/stores/contactsView.svelte'

  interface Props {
    onSelect: () => void
  }

  const { onSelect }: Props = $props()

  onMount(() => {
    contactSourcesStore.load()
  })

  // Two synthetic source ids:
  //   ''      → all (merged search)
  //   'local' → core local contacts
  // Plus the user's configured CardDAV sources, each with a UUID id.
  type SidebarItem = {
    id: string
    label: string
    icon: string
  }

  const sections = $derived(buildSections())

  function buildSections() {
    const builtins: SidebarItem[] = [
      { id: '', label: 'All', icon: 'mdi:account-multiple' },
      { id: 'local', label: 'Local (sent recipients)', icon: 'mdi:account' },
    ]
    const carddavItems: SidebarItem[] = contactSourcesStore.sources.map(s => ({
      id: s.id,
      label: s.name,
      icon: 'mdi:server',
    }))
    return [
      { items: builtins },
      { heading: 'Sources', items: carddavItems },
    ]
  }

  function pick(id: string) {
    selectSource(id)
    onSelect()
  }
</script>

<SourceSidebar
  title="Contacts"
  {sections}
  selectedId={contactsView.selectedSourceId}
  onSelect={pick}
>
  {#snippet item(it: SidebarItem, { active })}
    <SourceItem icon={it.icon} label={it.label} {active} onclick={() => pick(it.id)} />
  {/snippet}

  {#snippet sectionEmpty(_section: { heading?: string; items: SidebarItem[] })}
    <p class="mx-4 my-1 text-xs text-muted-foreground">No CardDAV sources configured.</p>
  {/snippet}
</SourceSidebar>
