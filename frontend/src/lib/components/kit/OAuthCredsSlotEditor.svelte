<script lang="ts">
  // OAuthCredsSlotEditor — single-slot OAuth credential editor primitive.
  // Used by Aerion core's "OAuth Credentials (advanced)" section (Settings →
  // Accounts) AND by each extension's settings dialog. Composed from existing
  // ui/input (type="password" for secret), ui/button, ui/select, and
  // ConfirmDialog — no new low-level inputs introduced.
  //
  // Props:
  //   configID            — the slot identifier (e.g., "google-mail")
  //   label               — display name (e.g., "Google Mail")
  //   secretRequired      — whether the slot needs a client_secret (true for
  //                          Google; false for Microsoft / PKCE)
  //   copyFromOptions     — list of { configID, label } that the user can
  //                          copy creds FROM via the picker. Empty list hides
  //                          the picker.
  //   onChanged           — fired after a successful save/clear/copy so the
  //                          parent can refresh its own state if needed.
  //
  // Behavior:
  //   - Fetches status (GetOAuthCredsStatus) on mount + after every mutation.
  //   - Shows status badge: "Default" or "Custom" + client_id fingerprint.
  //   - Edit → modal-style row with inputs + Save/Cancel.
  //   - Clear → confirm dialog → ClearOAuthCreds (no-op when no override).
  //   - Copy → picker (Select) of available source slots → CopyOAuthCreds.

  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import * as Select from '$lib/components/ui/select'
  import ConfirmDialog from '$lib/components/ui/confirm-dialog/ConfirmDialog.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import { GetOAuthCredsStatus, SetOAuthCreds, ClearOAuthCreds, CopyOAuthCreds } from '$wailsjs/go/app/App'
  // @ts-ignore - wailsjs bindings
  import type { app } from '$wailsjs/go/models'

  interface CopyFromOption {
    configID: string
    label: string
  }

  interface Props {
    configID: string
    label: string
    secretRequired?: boolean
    copyFromOptions?: CopyFromOption[]
    onChanged?: () => void
  }

  const {
    configID,
    label,
    secretRequired = true,
    copyFromOptions = [],
    onChanged,
  }: Props = $props()

  let status = $state<app.OAuthCredsStatus | null>(null)
  let loading = $state(true)
  let editing = $state(false)
  let clientID = $state('')
  let clientSecret = $state('')
  let saving = $state(false)
  let showClearConfirm = $state(false)

  async function refresh() {
    loading = true
    try {
      status = await GetOAuthCredsStatus(configID)
    } catch (err) {
      console.error('Failed to load OAuth creds status:', err)
      status = null
    } finally {
      loading = false
    }
  }

  onMount(refresh)

  function beginEdit() {
    // Inputs always start blank — values are never read back into the UI.
    clientID = ''
    clientSecret = ''
    editing = true
  }

  function cancelEdit() {
    editing = false
    clientID = ''
    clientSecret = ''
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
      toasts.success(`${label} credentials saved`)
      editing = false
      clientID = ''
      clientSecret = ''
      await refresh()
      onChanged?.()
    } catch (err) {
      console.error('Failed to save OAuth creds:', err)
      toasts.error('Failed to save credentials')
    } finally {
      saving = false
    }
  }

  async function clear() {
    try {
      await ClearOAuthCreds(configID)
      toasts.success(`${label} reset to default`)
      await refresh()
      onChanged?.()
    } catch (err) {
      console.error('Failed to clear OAuth creds:', err)
      toasts.error('Failed to clear credentials')
    }
  }

  async function copyFrom(fromConfigID: string) {
    try {
      await CopyOAuthCreds(fromConfigID, configID)
      toasts.success(`Copied credentials into ${label}`)
      await refresh()
      onChanged?.()
    } catch (err) {
      console.error('Failed to copy OAuth creds:', err)
      toasts.error(`Failed to copy credentials: ${(err as Error)?.message ?? err}`)
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
        {:else if status?.hasUserOverride}
          <span class="text-xs px-2 py-0.5 rounded bg-primary/15 text-primary">Custom</span>
        {:else if status?.hasShipped}
          <span class="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground">Default</span>
        {:else}
          <span class="text-xs px-2 py-0.5 rounded bg-destructive/15 text-destructive">Not configured</span>
        {/if}
        {#if status?.clientIdFingerprint}
          <code class="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">{status.clientIdFingerprint}</code>
        {/if}
      </div>
      <p class="text-xs text-muted-foreground mt-1 font-mono">{configID}</p>
    </div>
    {#if !editing}
      <div class="flex items-center gap-1 flex-shrink-0">
        <Button variant="outline" size="sm" onclick={beginEdit}>
          <Icon icon="mdi:pencil" class="w-4 h-4 mr-1" />
          Edit
        </Button>
        {#if status?.hasUserOverride}
          <Button variant="ghost" size="sm" onclick={() => { showClearConfirm = true }}>
            <Icon icon="mdi:close" class="w-4 h-4 mr-1" />
            Reset
          </Button>
        {/if}
      </div>
    {/if}
  </div>

  {#if editing}
    <div class="mt-4 space-y-3">
      <div>
        <Label for={`${configID}-client-id`}>Client ID</Label>
        <Input
          id={`${configID}-client-id`}
          type="text"
          bind:value={clientID}
          placeholder="paste client ID"
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
            placeholder="paste client secret"
            disabled={saving}
            autocomplete="new-password"
          />
        </div>
      {/if}
      <div class="flex items-center justify-end gap-2 pt-2">
        <Button variant="ghost" size="sm" onclick={cancelEdit} disabled={saving}>Cancel</Button>
        <Button size="sm" onclick={save} disabled={saving}>
          {#if saving}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
          {/if}
          Save
        </Button>
      </div>
    </div>
  {:else if copyFromOptions.length > 0}
    <div class="mt-3 flex items-center gap-2">
      <span class="text-xs text-muted-foreground">Copy from:</span>
      <Select.Root onValueChange={(value: string) => { if (value) copyFrom(value) }}>
        <Select.Trigger class="h-7 w-[220px] text-xs">
          <Select.Value placeholder="another slot…" />
        </Select.Trigger>
        <Select.Content>
          {#each copyFromOptions as opt (opt.configID)}
            <Select.Item value={opt.configID} label={opt.label} />
          {/each}
        </Select.Content>
      </Select.Root>
    </div>
  {/if}
</div>

<ConfirmDialog
  bind:open={showClearConfirm}
  title="Reset {label} to default?"
  description="Your custom Client ID and Secret will be deleted from this device. The slot will revert to the shipped default (if any)."
  confirmLabel="Reset"
  cancelLabel="Cancel"
  variant="destructive"
  onConfirm={clear}
/>
