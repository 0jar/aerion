<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Tabs from '$lib/components/ui/tabs'
  import { Button } from '$lib/components/ui/button'
  // @ts-ignore - wailsjs path
  import { GetReadReceiptResponsePolicy, SetReadReceiptResponsePolicy, GetMarkAsReadDelay, SetMarkAsReadDelay, GetMessageListDensity, SetMessageListDensity, GetThemeMode, SetThemeMode, GetShowTitleBar, SetShowTitleBar, GetRunBackground, SetRunBackground, GetStartHidden, SetStartHidden, GetAutostart, SetAutostart, GetLanguage, SetLanguage, GetComposerMode, SetComposerMode, GetMailtoMode, SetMailtoMode, GetComposerFormat, SetComposerFormat } from '../../../../wailsjs/go/app/App.js'
  import { addToast } from '$lib/stores/toast'
  import { setMessageListDensity as updateDensityStore, setThemeMode as updateThemeStore, setShowTitleBar as updateShowTitleBarStore, setRunBackground as updateRunBackgroundStore, setStartHidden as updateStartHiddenStore, setAutostart as updateAutostartStore, setLanguage as updateLanguageStore, setComposerMode as updateComposerModeStore, setMailtoMode as updateMailtoModeStore, setComposerFormat as updateComposerFormatStore, type MessageListDensity, type ThemeMode, type ComposerMode, type ComposerFormat } from '$lib/stores/settings.svelte'
  import { _ } from '$lib/i18n'
  import GeneralTab from './GeneralTab.svelte'
  import ComposerTab from './ComposerTab.svelte'
  import AccountsTab from './AccountsTab.svelte'
  import ContactsTab from './ContactsTab.svelte'
  import AboutTab from './AboutTab.svelte'

  interface Props {
    /** Whether the dialog is open */
    open?: boolean
    /** Callback when dialog should close */
    onClose?: () => void
  }

  let {
    open = $bindable(false),
    onClose,
  }: Props = $props()

  // Settings state
  let readReceiptResponsePolicy = $state<string>('ask')
  let markAsReadDelaySeconds = $state<number>(1) // Display in seconds, store in ms
  let messageListDensity = $state<string>('standard')
  let themeMode = $state<string>('system')
  let showTitleBar = $state<boolean>(true)
  let runBackground = $state<boolean>(false)
  let startHidden = $state<boolean>(false)
  let autostart = $state<boolean>(false)
  let language = $state<string>('')
  let composerMode = $state<string>('inline')
  let mailtoMode = $state<string>('inline')
  let composerFormat = $state<string>('rich')
  let loading = $state(true)
  let saving = $state(false)
  let activeTab = $state('general')

  // Load settings on mount
  onMount(async () => {
    await loadSettings()
  })

  // Also load when dialog opens
  $effect(() => {
    if (open) {
      loadSettings()
    }
  })

  async function loadSettings() {
    loading = true
    try {
      const [policy, delayMs, density, theme, titleBar, runBg, startHid, autoSt, lang, comp, mail, compFmt] = await Promise.all([
        GetReadReceiptResponsePolicy(),
        GetMarkAsReadDelay(),
        GetMessageListDensity(),
        GetThemeMode(),
        GetShowTitleBar(),
        GetRunBackground(),
        GetStartHidden(),
        GetAutostart(),
        GetLanguage(),
        GetComposerMode(),
        GetMailtoMode(),
        GetComposerFormat(),
      ])
      readReceiptResponsePolicy = policy
      // Convert ms to seconds for display
      markAsReadDelaySeconds = delayMs < 0 ? -1 : delayMs / 1000
      messageListDensity = density
      themeMode = theme
      showTitleBar = titleBar
      runBackground = runBg
      startHidden = startHid
      autostart = autoSt
      language = lang
      composerMode = comp || 'inline'
      mailtoMode = mail || 'inline'
      composerFormat = compFmt || 'rich'
    } catch (err) {
      console.error('Failed to load settings:', err)
    } finally {
      loading = false
    }
  }

  async function handleSave() {
    saving = true
    try {
      // Convert seconds to ms for storage
      const delayMs = markAsReadDelaySeconds < 0 ? -1 : Math.round(markAsReadDelaySeconds * 1000)

      // Save settings sequentially to avoid SQLite lock conflicts
      await SetReadReceiptResponsePolicy(readReceiptResponsePolicy)
      await SetMarkAsReadDelay(delayMs)
      await SetMessageListDensity(messageListDensity)
      await SetThemeMode(themeMode)
      await SetShowTitleBar(showTitleBar)
      await SetRunBackground(runBackground)
      await SetStartHidden(startHidden)
      await SetAutostart(autostart)
      if (language) {
        await SetLanguage(language)
      }
      await SetComposerMode(composerMode)
      await SetMailtoMode(mailtoMode)
      await SetComposerFormat(composerFormat)
      // Update the reactive stores so UI updates immediately
      updateDensityStore(messageListDensity as MessageListDensity)
      updateThemeStore(themeMode as ThemeMode)
      updateShowTitleBarStore(showTitleBar)
      updateRunBackgroundStore(runBackground)
      updateStartHiddenStore(startHidden)
      updateAutostartStore(autostart)
      if (language) {
        updateLanguageStore(language)
      }
      updateComposerModeStore(composerMode as ComposerMode)
      updateMailtoModeStore(mailtoMode as ComposerMode)
      updateComposerFormatStore(composerFormat as ComposerFormat)
      addToast({
        type: 'success',
        message: $_('toast.settingsSaved'),
      })
      open = false
      onClose?.()
    } catch (err) {
      console.error('Failed to save settings:', err)
      addToast({
        type: 'error',
        message: $_('toast.failedToSaveSettings'),
      })
    } finally {
      saving = false
    }
  }

  function handleCancel() {
    open = false
    onClose?.()
  }

  function handleOpenChange(isOpen: boolean) {
    open = isOpen
    if (!isOpen) {
      onClose?.()
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={handleOpenChange}>
  <Dialog.Content class="max-w-2xl" preventCloseAutoFocus>
    <Dialog.Header>
      <Dialog.Title>{$_('settings.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('settings.description')}
      </Dialog.Description>
    </Dialog.Header>

    {#if loading}
      <div class="flex items-center justify-center py-8">
        <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    {:else}
      <Tabs.Root bind:value={activeTab} class="w-full">
        <Tabs.List class="grid w-full grid-cols-5">
          <Tabs.Trigger value="general" class="flex items-center gap-2">
            <Icon icon="mdi:cog" class="w-4 h-4" />
            {$_('settings.general')}
          </Tabs.Trigger>
          <Tabs.Trigger value="composer" class="flex items-center gap-2">
            <Icon icon="mdi:email-edit-outline" class="w-4 h-4" />
            {$_('settings.composer')}
          </Tabs.Trigger>
          <Tabs.Trigger value="accounts" class="flex items-center gap-2">
            <Icon icon="mdi:email-multiple" class="w-4 h-4" />
            {$_('settings.accounts')}
          </Tabs.Trigger>
          <Tabs.Trigger value="contacts" class="flex items-center gap-2">
            <Icon icon="mdi:contacts" class="w-4 h-4" />
            {$_('settings.contacts')}
          </Tabs.Trigger>
          <Tabs.Trigger value="about" class="flex items-center gap-2">
            <Icon icon="mdi:information-outline" class="w-4 h-4" />
            {$_('settings.about')}
          </Tabs.Trigger>
        </Tabs.List>

        <div class="mt-4 h-[350px] overflow-y-auto">
          <Tabs.Content value="general" class="mt-0">
            <GeneralTab
              bind:markAsReadDelaySeconds
              bind:messageListDensity
              bind:themeMode
              bind:showTitleBar
              bind:runBackground
              bind:startHidden
              bind:autostart
              bind:language
              onDelayChange={(v) => markAsReadDelaySeconds = v}
              onDensityChange={(v) => messageListDensity = v}
              onThemeChange={(v) => themeMode = v}
              onShowTitleBarChange={(v) => showTitleBar = v}
              onRunBackgroundChange={(v) => { runBackground = v; if (!v) startHidden = false }}
              onStartHiddenChange={(v) => { startHidden = v; if (v) runBackground = true }}
              onAutostartChange={(v) => autostart = v}
              onLanguageChange={(v) => language = v}
            />
          </Tabs.Content>

          <Tabs.Content value="composer" class="mt-0">
            <ComposerTab
              bind:composerMode
              bind:mailtoMode
              bind:composerFormat
              bind:readReceiptResponsePolicy
              onComposerModeChange={(v) => { composerMode = v; if (v === 'detached') mailtoMode = 'detached' }}
              onMailtoModeChange={(v) => mailtoMode = v}
              onFormatChange={(v) => composerFormat = v}
              onPolicyChange={(v) => readReceiptResponsePolicy = v}
            />
          </Tabs.Content>

          <Tabs.Content value="accounts" class="mt-0">
            <AccountsTab />
          </Tabs.Content>

          <Tabs.Content value="contacts" class="mt-0">
            <ContactsTab />
          </Tabs.Content>

          <Tabs.Content value="about" class="mt-0">
            <AboutTab />
          </Tabs.Content>
        </div>
      </Tabs.Root>

      <!-- Actions - show Save/Cancel on General and Composer tabs -->
      {#if activeTab === 'general' || activeTab === 'composer'}
        <div class="flex items-center justify-end gap-2 pt-4 border-t border-border">
          <Button variant="ghost" onclick={handleCancel} disabled={saving}>
            {$_('common.cancel')}
          </Button>
          <Button onclick={handleSave} disabled={saving}>
            {#if saving}
              <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
            {/if}
            {$_('common.save')}
          </Button>
        </div>
      {:else}
        <div class="flex items-center justify-end gap-2 pt-4 border-t border-border">
          <Button variant="ghost" onclick={handleCancel}>
            {$_('common.close')}
          </Button>
        </div>
      {/if}
    {/if}
  </Dialog.Content>
</Dialog.Root>
