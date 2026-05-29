<script lang="ts">
  // IncrementalConsentDialog — generic prompt that fires when an extension's
  // HTTPClient call hits ErrAdditionalConsentRequired (i.e., the user's
  // account doesn't have the requested scopes under the extension's client
  // config yet).
  //
  // Phase 2b.1: SCAFFOLDED. The dialog listens for the
  // `oauth:incremental-consent-required` event from the host but no
  // production code path emits it yet. Phase 2b.3 lights this up when the
  // Google People / MS Graph write paths first call core.Auth().HTTPClient
  // and surface the consent requirement back to the host.
  //
  // Single dialog reused across all extensions. Copy comes from manifest +
  // missing-scope strings — never hard-codes "contacts" or "calendar".

  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs runtime
  import { EventsOn } from '$wailsjs/runtime/runtime'

  interface ConsentRequest {
    accountID: string
    clientConfigID: string  // e.g., "google-contacts"
    extensionID: string     // e.g., "contacts"
    extensionLabel: string  // e.g., "Contacts"
    missingScopes: string[] // scope resource strings
  }

  let open = $state(false)
  let request = $state<ConsentRequest | null>(null)
  let connecting = $state(false)

  onMount(() => {
    EventsOn('oauth:incremental-consent-required', (payload: ConsentRequest) => {
      request = payload
      open = true
    })
  })

  function close() {
    open = false
    request = null
    connecting = false
  }

  async function connect() {
    if (!request) return
    connecting = true
    try {
      // Phase 2b.3 will wire this to a Wails method that opens the OAuth
      // flow with the extension's clientConfigID + missing scopes, stores
      // the resulting tokens under that client config, then retries the
      // original action that triggered the consent. For 2b.1 this is a
      // placeholder — flow isn't wired yet.
      toasts.info('Incremental consent flow lands in a later release.')
      close()
    } catch (err) {
      console.error('Failed to start incremental consent flow:', err)
      toasts.error(`Failed to start consent flow: ${(err as Error)?.message ?? err}`)
      connecting = false
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-md">
    <Dialog.Header>
      <Dialog.Title>Additional permission needed</Dialog.Title>
      <Dialog.Description>
        {#if request}
          {request.extensionLabel} needs additional access to your account
          before it can perform this action.
        {/if}
      </Dialog.Description>
    </Dialog.Header>

    {#if request}
      <div class="space-y-3 mt-2">
        <p class="text-sm text-muted-foreground">
          You'll be sent to your OAuth provider to grant the following
          permission{request.missingScopes.length === 1 ? '' : 's'}:
        </p>
        <ul class="space-y-1">
          {#each request.missingScopes as scope (scope)}
            <li class="flex items-start gap-2 text-sm">
              <Icon icon="mdi:shield-key-outline" class="w-4 h-4 mt-0.5 text-primary flex-shrink-0" />
              <code class="text-xs bg-muted px-1.5 py-0.5 rounded break-all">{scope}</code>
            </li>
          {/each}
        </ul>
        <p class="text-xs text-muted-foreground">
          Your existing mail access stays intact — this grants a separate,
          extension-specific token. You can revoke it any time from your
          OAuth provider's account settings.
        </p>
      </div>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={connecting}>Not now</Button>
      <Button onclick={connect} disabled={connecting || !request}>
        {#if connecting}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        Connect
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
