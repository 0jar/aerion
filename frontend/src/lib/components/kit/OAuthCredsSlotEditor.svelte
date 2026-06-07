<script lang="ts">
  // OAuthCredsSlotEditor — single-slot OAuth credential editor primitive.
  // Used by Aerion core's "OAuth Credentials (advanced)" section (Settings →
  // Accounts) AND by each extension's settings dialog.
  //
  // Props:
  //   configID            — the slot identifier (e.g., "google-mail",
  //                         "google-contacts")
  //   extensionID         — the manifest id of the consuming extension
  //                         (e.g., "contacts", "calendar"). Omit (or pass "")
  //                         for core/mail's settings UI — then the backend
  //                         skips the manifest lookup and the "Aerion mail
  //                         client" option never appears.
  //   label               — display name (e.g., "Google Mail")
  //   secretRequired      — whether the slot needs a client_secret (true for
  //                         Google; false for Microsoft / PKCE)
  //
  // UX:
  //   Dropdown enumerates the available credential sources for this slot.
  //   Choice IDs come from the backend (GetOAuthCredsChoices); persistence
  //   goes through SetOAuthCredsChoice. Possible IDs today:
  //     - "custom"          — user-pasted client_id/secret. Selecting reveals
  //                           the edit form.
  //     - "aerion-shipped"  — the slot's own shipped client (compiled in via
  //                           the extension's .env / Makefile ldflags). Labeled
  //                           by the backend per slot ("Aerion - Google",
  //                           "Aerion - Microsoft", "Aerion testing", etc.).
  //     - "aerion-mail"     — reuse the core mail OAuth slot for scopes the
  //                           extension manifest declares as core-routable
  //                           (first_party_uses_core_for_scopes).
  //
  //   Picking a non-custom option writes the choice via SetOAuthCredsChoice
  //   (which clears any user override and sets/clears the slot alias as
  //   appropriate). Picking Custom shows the empty form; user pastes + Saves
  //   → SetOAuthCreds writes the user override AND SetOAuthCredsChoice
  //   ensures any stale alias is cleared.

  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import * as Select from '$lib/components/ui/select'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import { GetOAuthCredsChoices, SetOAuthCreds, SetOAuthCredsChoice } from '$wailsjs/go/app/App'
  // @ts-ignore - wailsjs bindings
  import type { app } from '$wailsjs/go/models'

  interface Props {
    configID: string
    label: string
    extensionID?: string
    secretRequired?: boolean
  }

  const { configID, label, extensionID = '', secretRequired = true }: Props = $props()

  let choices = $state<app.OAuthCredsChoices | null>(null)
  let loading = $state(true)
  let mode = $state<string>('custom')
  let clientID = $state('')
  let clientSecret = $state('')
  let saving = $state(false)

  function currentChoiceLabel(): string {
    const match = choices?.choices.find(c => c.id === mode)
    if (match) return match.label
    return 'Custom'
  }

  // Status badge driven by the currently-selected mode rather than two
  // independent has* booleans. Maps to the same three visual states the
  // previous version had: Custom / Aerion / Not configured.
  function statusBadgeKind(): 'custom' | 'aerion' | 'unset' {
    if (loading) return 'unset'
    if (mode === 'custom' && choices?.hasUserOverride) return 'custom'
    if (mode !== 'custom') return 'aerion'
    return 'unset'
  }

  async function refresh() {
    loading = true
    try {
      choices = await GetOAuthCredsChoices(configID, extensionID)
      mode = choices?.current || 'custom'
    } catch (err) {
      console.error('Failed to load OAuth creds choices:', err)
      choices = null
      mode = 'custom'
    } finally {
      loading = false
    }
  }

  onMount(refresh)

  // Dropdown change handler. Custom shows the blank edit form; any other
  // choice gets persisted server-side via SetOAuthCredsChoice (which clears
  // any conflicting custom override and aligns the alias state).
  async function setMode(value: string | undefined) {
    if (!value) return
    if (value === mode) return
    const next = value
    mode = next
    if (next === 'custom') {
      clientID = ''
      clientSecret = ''
      try {
        await SetOAuthCredsChoice(configID, 'custom')
      } catch (err) {
        // Failing the alias-clear here doesn't block the user — they'll
        // either save Custom creds (which establishes the override) or
        // switch back. Surface as a console warning, not a toast.
        console.warn('Failed to clear OAuth slot alias:', err)
      }
      return
    }
    try {
      await SetOAuthCredsChoice(configID, next)
      const labelText = choices?.choices.find(c => c.id === next)?.label ?? next
      toasts.success(`${label} is now using ${labelText}`)
      await refresh()
    } catch (err) {
      console.error('Failed to switch OAuth choice:', err)
      toasts.error(`Failed to switch credentials: ${(err as Error)?.message ?? err}`)
      await refresh()
    }
  }

  async function save() {
    if (!clientID.trim()) {
      toasts.error('Client ID is required')
      return
    }
    if (secretRequired && !clientSecret.trim()) {
      toasts.error('Client Secret is required')
      return
    }
    saving = true
    try {
      await SetOAuthCreds(configID, clientID.trim(), clientSecret.trim())
      // SetOAuthCreds writes the user override; ensure any alias from a
      // previous "Aerion mail client" choice is cleared.
      try {
        await SetOAuthCredsChoice(configID, 'custom')
      } catch (err) {
        console.warn('Failed to clear OAuth slot alias after Custom save:', err)
      }
      toasts.success(`${label} credentials saved`)
      clientID = ''
      clientSecret = ''
      await refresh()
    } catch (err) {
      console.error('Failed to save OAuth creds:', err)
      toasts.error('Failed to save credentials')
    } finally {
      saving = false
    }
  }
</script>

<div class="border border-border rounded-md p-4 bg-card">
  <div class="flex items-start justify-between gap-3">
    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-2 flex-wrap">
        <h4 class="font-medium text-foreground">{label}</h4>
        {#if loading}
          <Icon icon="mdi:loading" class="w-3.5 h-3.5 animate-spin text-muted-foreground" />
        {:else if statusBadgeKind() === 'custom'}
          <span class="text-xs px-2 py-0.5 rounded bg-primary/15 text-primary">Custom</span>
        {:else if statusBadgeKind() === 'aerion'}
          <span class="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground">Aerion</span>
        {:else}
          <span class="text-xs px-2 py-0.5 rounded bg-destructive/15 text-destructive">Not configured</span>
        {/if}
        {#if choices?.clientIdFingerprint}
          <code class="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">{choices.clientIdFingerprint}</code>
        {/if}
      </div>
      <p class="text-xs text-muted-foreground mt-1 font-mono">{configID}</p>
    </div>
  </div>

  <div class="mt-3 flex items-center gap-2">
    <Label class="text-xs text-muted-foreground">Client ID/Secret:</Label>
    <Select.Root value={mode} onValueChange={setMode}>
      <Select.Trigger class="h-8 w-[220px] text-sm">
        <Select.Value placeholder="Custom">
          {currentChoiceLabel()}
        </Select.Value>
      </Select.Trigger>
      <Select.Content>
        {#each (choices?.choices ?? []) as choice (choice.id)}
          <Select.Item value={choice.id} label={choice.label} />
        {/each}
      </Select.Content>
    </Select.Root>
  </div>

  {#if mode === 'custom'}
    <div class="mt-4 space-y-3">
      <div>
        <Label for={`${configID}-client-id`}>Client ID</Label>
        <Input
          id={`${configID}-client-id`}
          type="text"
          bind:value={clientID}
          placeholder={choices?.hasUserOverride ? 'paste a new Client ID to replace' : 'paste Client ID'}
          disabled={saving}
          autocomplete="off"
        />
      </div>
      {#if secretRequired}
        <div>
          <Label for={`${configID}-client-secret`}>Client Secret</Label>
          <Input
            id={`${configID}-client-secret`}
            type="password"
            bind:value={clientSecret}
            placeholder={choices?.hasUserOverride ? 'paste a new Client Secret to replace' : 'paste Client Secret'}
            disabled={saving}
            autocomplete="new-password"
          />
        </div>
      {/if}
      <div class="flex items-center justify-end gap-2 pt-2">
        <Button size="sm" onclick={save} disabled={saving}>
          {#if saving}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
          {/if}
          Save
        </Button>
      </div>
    </div>
  {/if}
</div>
