// Extension registry — frontend cache of enabled extensions, rail tabs, and
// account-setup hooks. Loaded once at app startup; refresh() re-pulls from the
// backend after Settings toggles an extension or after an account is added.
//
// IMPORTANT: read access goes through plain exported FUNCTIONS, not via an
// object with getters. Svelte 5's reactivity tracker doesn't reliably see
// through getter properties on plain object literals — using them inside a
// template made the whole rail re-render on every tick, hogging the main
// thread and dropping IPC events. Plain functions are the established pattern
// elsewhere in the codebase (see getActiveExtension in uiState.svelte.ts).

// @ts-ignore - wailsjs bindings
import { ListEnabledExtensions, ListExtensionRailTabs, ListAccountSetupHooksForProvider } from '../../../wailsjs/go/app/App'
// @ts-ignore - wailsjs bindings
import type { v1 } from '../../../wailsjs/go/models'

let enabledExtensions = $state<string[]>([])
let railTabs = $state<v1.RailTabRequest[]>([])

export function getEnabledExtensions(): string[] {
  return enabledExtensions
}

export function getRailTabs(): v1.RailTabRequest[] {
  return railTabs
}

// Rail renders when there's at least one enabled extension to switch between
// Mail and. (Mail is always-on but not in enabledExtensions, so one enabled
// extension = two rail items: Mail + that extension.)
export function isRailVisible(): boolean {
  return enabledExtensions.length >= 1
}

export function isExtensionEnabled(name: string): boolean {
  return enabledExtensions.includes(name)
}

export async function refreshExtensionRegistry(): Promise<void> {
  try {
    enabledExtensions = await ListEnabledExtensions() || []
    railTabs = await ListExtensionRailTabs() || []
  } catch (err) {
    console.error('Failed to refresh extension registry:', err)
    enabledExtensions = []
    railTabs = []
  }
}

// Provider-keyed hook cache. Hooks change rarely (only when extension state
// changes), but we re-fetch per provider on demand since the result is small.
export async function loadAccountSetupHooks(provider: string): Promise<v1.AccountSetupHookRequest[]> {
  try {
    const hooks = await ListAccountSetupHooksForProvider(provider)
    return hooks || []
  } catch (err) {
    console.error('Failed to load account setup hooks for provider', provider, err)
    return []
  }
}
