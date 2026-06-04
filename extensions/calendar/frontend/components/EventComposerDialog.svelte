<script lang="ts">
  // EventComposerDialog — create + edit form for local events.
  //
  // Two modes: 'create' (build new EventInput) and 'edit' (prefill from
  // an existing event + pass scope for recurring updates). The calendar
  // picker only lists local-source calendars; CalDAV writes are Phase 2.
  //
  // Date/time inputs render in the user's display timezone via
  // calendarSettings.effectiveTimezone. On save, the local-tz datetime is
  // converted to a UTC unix instant via date-fns-tz's fromZonedTime so
  // the backend always stores absolute time.

  import { _ } from 'svelte-i18n'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import Icon from '@iconify/svelte'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { calendarSources } from '$extensions/calendar/frontend/stores/calendarSources.svelte'
  import { calendarSettings } from '$extensions/calendar/frontend/stores/calendarSettings.svelte'
  import { fromZonedTime, toZonedTime } from 'date-fns-tz'
  // @ts-ignore - wailsjs bindings
  import { Calendar_CreateEvent, Calendar_UpdateEvent } from '$wailsjs/go/app/App.js'
  // @ts-ignore - wailsjs bindings
  import type { backend } from '$wailsjs/go/models'

  type ComposerMode = 'create' | 'edit'

  interface Props {
    open: boolean
    mode?: ComposerMode
    existing?: backend.Event | null
    scope?: 'this' | 'this-and-future' | 'all'
    defaultStart?: Date | null
    defaultCalendarId?: string
    onClose?: () => void
    onSaved?: () => void
  }

  let {
    open = $bindable(false),
    mode = 'create',
    existing = null,
    scope = 'all',
    defaultStart = null,
    defaultCalendarId = '',
    onClose,
    onSaved,
  }: Props = $props()

  let summary = $state('')
  let calendarId = $state('')
  let isAllDay = $state(false)
  let startDate = $state('')
  let startTime = $state('')
  let endDate = $state('')
  let endTime = $state('')
  let location = $state('')
  let description = $state('')

  let recurrenceFreq = $state('')
  let recurrenceEnd = $state('never')
  let recurrenceUntilDate = $state('')
  let recurrenceCount = $state(10)

  let reminderChoice = $state('none')
  let reminderCustomMinutes = $state(15)

  let submitting = $state(false)
  let errorMessage = $state('')

  const localCalendars = $derived.by(() => {
    const out: { id: string; name: string }[] = []
    for (const src of calendarSources.sources) {
      if (src.type !== 'local') continue
      for (const cal of calendarSources.calendarsBySource[src.id] || []) {
        out.push({ id: cal.id, name: cal.displayName })
      }
    }
    return out
  })

  $effect(() => {
    if (!open) return
    dialogGuardOpen()
    return () => dialogGuardClose()
  })

  $effect(() => {
    if (!open) return
    errorMessage = ''
    submitting = false
    initForm()
  })

  function initForm() {
    if (mode === 'edit' && existing) {
      initFromExisting(existing)
      return
    }
    initCreateDefaults()
  }

  function initFromExisting(ev: backend.Event) {
    const tz = calendarSettings.effectiveTimezone
    calendarId = ev.calendarId
    summary = ev.summary || ''
    location = ev.location || ''
    description = ev.description || ''
    isAllDay = !!ev.isAllDay
    const startInTz = toZonedTime(new Date(ev.dtstartUnix * 1000), tz)
    const endInTz = toZonedTime(new Date(ev.dtendUnix * 1000), tz)
    startDate = formatYMD(startInTz)
    startTime = formatHM(startInTz)
    endDate = formatYMD(endInTz)
    endTime = formatHM(endInTz)
    parseRRule(ev.rruleText || '')
    reminderChoice = 'none'
  }

  function initCreateDefaults() {
    const tz = calendarSettings.effectiveTimezone
    calendarId = defaultCalendarId || localCalendars[0]?.id || ''
    const ref = defaultStart ?? new Date()
    const refInTz = toZonedTime(ref, tz)
    const isDefaultNow = defaultStart === null
    if (isDefaultNow) {
      const min = refInTz.getMinutes()
      if (min < 30) refInTz.setMinutes(30, 0, 0)
      if (min >= 30) {
        refInTz.setMinutes(0, 0, 0)
        refInTz.setHours(refInTz.getHours() + 1)
      }
    }
    const endRef = new Date(refInTz)
    endRef.setHours(endRef.getHours() + 1)
    summary = ''
    location = ''
    description = ''
    isAllDay = false
    startDate = formatYMD(refInTz)
    startTime = formatHM(refInTz)
    endDate = formatYMD(endRef)
    endTime = formatHM(endRef)
    recurrenceFreq = ''
    recurrenceEnd = 'never'
    recurrenceUntilDate = ''
    recurrenceCount = 10
    reminderChoice = 'none'
    reminderCustomMinutes = 15
  }

  function parseRRule(text: string) {
    if (!text) {
      recurrenceFreq = ''
      return
    }
    const body = text.startsWith('RRULE:') ? text.slice(6) : text
    const parts: Record<string, string> = {}
    for (const seg of body.split(';')) {
      const eq = seg.indexOf('=')
      if (eq <= 0) continue
      parts[seg.slice(0, eq).toUpperCase()] = seg.slice(eq + 1)
    }
    recurrenceFreq = parts.FREQ || ''
    if (parts.UNTIL) {
      recurrenceEnd = 'date'
      const m = parts.UNTIL.match(/^(\d{4})(\d{2})(\d{2})/)
      if (m) recurrenceUntilDate = `${m[1]}-${m[2]}-${m[3]}`
      return
    }
    if (parts.COUNT) {
      recurrenceEnd = 'count'
      const n = Number(parts.COUNT)
      if (Number.isFinite(n) && n > 0) recurrenceCount = n
      return
    }
    recurrenceEnd = 'never'
  }

  function formatYMD(d: Date): string {
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${y}-${m}-${day}`
  }

  function formatHM(d: Date): string {
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }

  function buildUnix(dateStr: string, timeStr: string, allDay: boolean): number {
    const [y, m, d] = dateStr.split('-').map(Number)
    let hh = 0
    let mm = 0
    if (!allDay) {
      const tparts = (timeStr || '00:00').split(':').map(Number)
      hh = tparts[0] || 0
      mm = tparts[1] || 0
    }
    const wall = new Date(y, (m || 1) - 1, d || 1, hh, mm, 0, 0)
    const utc = fromZonedTime(wall, calendarSettings.effectiveTimezone)
    return Math.floor(utc.getTime() / 1000)
  }

  function reminderMinutes(): number {
    if (reminderChoice === 'custom') return reminderCustomMinutes
    if (reminderChoice === 'none') return -1
    const n = Number(reminderChoice)
    return Number.isFinite(n) ? n : -1
  }

  async function handleSave() {
    if (submitting) return
    errorMessage = ''
    if (!summary.trim()) {
      errorMessage = $_('calendar.composer.errorSummaryRequired')
      return
    }
    if (!calendarId) {
      errorMessage = $_('calendar.composer.errorCalendarRequired')
      return
    }
    if (!startDate) {
      errorMessage = $_('calendar.composer.errorStartRequired')
      return
    }

    const dtstartUnix = buildUnix(startDate, startTime, isAllDay)
    const dtendUnix = buildUnix(endDate || startDate, endTime || startTime, isAllDay)
    if (dtendUnix < dtstartUnix) {
      errorMessage = $_('calendar.composer.errorEndBeforeStart')
      return
    }

    const input = {
      calendarId,
      summary: summary.trim(),
      description: description.trim() || undefined,
      location: location.trim() || undefined,
      dtstartUnix,
      dtendUnix,
      isAllDay: isAllDay || undefined,
      recurrence: buildRecurrenceSpec(),
      reminder: buildReminderSpec(),
    } as backend.EventInput

    submitting = true
    try {
      if (mode === 'edit' && existing) {
        await Calendar_UpdateEvent(
          { eventId: existing.id, ...input } as backend.EventUpdateInput,
          scope,
        )
        toasts.success($_('calendar.composer.toastUpdated'))
      }
      if (mode !== 'edit' || !existing) {
        await Calendar_CreateEvent(input)
        toasts.success($_('calendar.composer.toastCreated'))
      }
      // Clear submitting before close() so the guard inside close()
      // (which blocks user-initiated cancels during a request) doesn't
      // short-circuit the auto-close.
      submitting = false
      onSaved?.()
      close()
    } catch (err) {
      errorMessage = (err as Error)?.message ?? String(err)
    } finally {
      submitting = false
    }
  }

  function buildRecurrenceSpec(): backend.RecurrenceSpec | undefined {
    if (!recurrenceFreq) return undefined
    const spec = { freq: recurrenceFreq } as backend.RecurrenceSpec
    if (recurrenceEnd === 'date' && recurrenceUntilDate) {
      spec.untilUnix = buildUnix(recurrenceUntilDate, '23:59', true)
    }
    if (recurrenceEnd === 'count' && recurrenceCount > 0) {
      spec.count = recurrenceCount
    }
    return spec
  }

  function buildReminderSpec(): backend.ReminderSpec | undefined {
    const m = reminderMinutes()
    if (m < 0) return undefined
    return { offsetMinutes: m } as backend.ReminderSpec
  }

  function close() {
    if (submitting) return
    open = false
    onClose?.()
  }

  function recurrenceFreqLabel(freq: string): string {
    if (freq === 'DAILY') return $_('calendar.composer.recurrence.daily')
    if (freq === 'WEEKLY') return $_('calendar.composer.recurrence.weekly')
    if (freq === 'MONTHLY') return $_('calendar.composer.recurrence.monthly')
    if (freq === 'YEARLY') return $_('calendar.composer.recurrence.yearly')
    return $_('calendar.composer.recurrence.none')
  }

  function recurrenceEndLabel(v: string): string {
    if (v === 'date') return $_('calendar.composer.recurrence.endOnDate')
    if (v === 'count') return $_('calendar.composer.recurrence.endAfterCount')
    return $_('calendar.composer.recurrence.endNever')
  }

  function reminderLabel(): string {
    if (reminderChoice === 'none') return $_('calendar.composer.reminder.none')
    if (reminderChoice === 'custom') {
      return $_('calendar.composer.reminder.customLabel', { values: { n: reminderCustomMinutes } })
    }
    if (reminderChoice === '0') return $_('calendar.composer.reminder.atTime')
    if (reminderChoice === '5') return $_('calendar.composer.reminder.fiveMin')
    if (reminderChoice === '15') return $_('calendar.composer.reminder.fifteenMin')
    if (reminderChoice === '30') return $_('calendar.composer.reminder.thirtyMin')
    if (reminderChoice === '60') return $_('calendar.composer.reminder.oneHour')
    if (reminderChoice === '1440') return $_('calendar.composer.reminder.oneDay')
    return reminderChoice
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) close() }}>
  <Dialog.Content class="max-w-lg">
    <Dialog.Header>
      <Dialog.Title>
        {mode === 'edit' ? $_('calendar.composer.titleEdit') : $_('calendar.composer.titleCreate')}
      </Dialog.Title>
    </Dialog.Header>

    <div class="space-y-3 mt-2 max-h-[60vh] overflow-y-auto pr-1">
      <div>
        <Label for="cal-composer-summary">{$_('calendar.composer.summaryLabel')}</Label>
        <Input
          id="cal-composer-summary"
          type="text"
          placeholder={$_('calendar.composer.summaryPlaceholder')}
          bind:value={summary}
          disabled={submitting}
        />
      </div>

      <div>
        <Label>{$_('calendar.composer.calendarLabel')}</Label>
        {#if localCalendars.length === 0}
          <p class="text-xs text-destructive mt-1">{$_('calendar.composer.noLocalCalendars')}</p>
        {/if}
        {#if localCalendars.length > 0}
          <Select.Root value={calendarId} onValueChange={(v) => { if (v) calendarId = v }}>
            <Select.Trigger class="h-9">
              {localCalendars.find(c => c.id === calendarId)?.name ?? ''}
            </Select.Trigger>
            <Select.Content>
              {#each localCalendars as c (c.id)}
                <Select.Item value={c.id} label={c.name} />
              {/each}
            </Select.Content>
          </Select.Root>
        {/if}
      </div>

      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={isAllDay} disabled={submitting} class="accent-primary" />
        <span>{$_('calendar.composer.allDayLabel')}</span>
      </label>

      <div class="grid grid-cols-2 gap-2">
        <div>
          <Label for="cal-composer-startdate">{$_('calendar.composer.startDateLabel')}</Label>
          <Input id="cal-composer-startdate" type="date" bind:value={startDate} disabled={submitting} />
        </div>
        {#if !isAllDay}
          <div>
            <Label for="cal-composer-starttime">{$_('calendar.composer.startTimeLabel')}</Label>
            <Input id="cal-composer-starttime" type="time" bind:value={startTime} disabled={submitting} />
          </div>
        {/if}
      </div>

      <div class="grid grid-cols-2 gap-2">
        <div>
          <Label for="cal-composer-enddate">{$_('calendar.composer.endDateLabel')}</Label>
          <Input id="cal-composer-enddate" type="date" bind:value={endDate} disabled={submitting} />
        </div>
        {#if !isAllDay}
          <div>
            <Label for="cal-composer-endtime">{$_('calendar.composer.endTimeLabel')}</Label>
            <Input id="cal-composer-endtime" type="time" bind:value={endTime} disabled={submitting} />
          </div>
        {/if}
      </div>

      <div>
        <Label for="cal-composer-location">{$_('calendar.composer.locationLabel')}</Label>
        <Input id="cal-composer-location" type="text" bind:value={location} disabled={submitting} />
      </div>

      <div>
        <Label for="cal-composer-description">{$_('calendar.composer.descriptionLabel')}</Label>
        <textarea
          id="cal-composer-description"
          bind:value={description}
          disabled={submitting}
          class="w-full h-20 px-2 py-1 text-sm border border-border rounded bg-background focus:outline-none focus:ring-2 focus:ring-primary/50"
        ></textarea>
      </div>

      <div>
        <Label>{$_('calendar.composer.recurrenceLabel')}</Label>
        <Select.Root value={recurrenceFreq} onValueChange={(v) => { recurrenceFreq = v ?? '' }}>
          <Select.Trigger class="h-9">
            {recurrenceFreqLabel(recurrenceFreq)}
          </Select.Trigger>
          <Select.Content>
            <Select.Item value="" label={$_('calendar.composer.recurrence.none')} />
            <Select.Item value="DAILY" label={$_('calendar.composer.recurrence.daily')} />
            <Select.Item value="WEEKLY" label={$_('calendar.composer.recurrence.weekly')} />
            <Select.Item value="MONTHLY" label={$_('calendar.composer.recurrence.monthly')} />
            <Select.Item value="YEARLY" label={$_('calendar.composer.recurrence.yearly')} />
          </Select.Content>
        </Select.Root>

        {#if recurrenceFreq}
          <div class="mt-2 grid grid-cols-2 gap-2">
            <Select.Root value={recurrenceEnd} onValueChange={(v) => { if (v) recurrenceEnd = v }}>
              <Select.Trigger class="h-9">
                {recurrenceEndLabel(recurrenceEnd)}
              </Select.Trigger>
              <Select.Content>
                <Select.Item value="never" label={$_('calendar.composer.recurrence.endNever')} />
                <Select.Item value="date" label={$_('calendar.composer.recurrence.endOnDate')} />
                <Select.Item value="count" label={$_('calendar.composer.recurrence.endAfterCount')} />
              </Select.Content>
            </Select.Root>
            {#if recurrenceEnd === 'date'}
              <Input type="date" bind:value={recurrenceUntilDate} disabled={submitting} />
            {/if}
            {#if recurrenceEnd === 'count'}
              <Input type="number" min="1" bind:value={recurrenceCount} disabled={submitting} />
            {/if}
          </div>
        {/if}
      </div>

      <div>
        <Label>{$_('calendar.composer.reminderLabel')}</Label>
        <Select.Root value={reminderChoice} onValueChange={(v) => { if (v) reminderChoice = v }}>
          <Select.Trigger class="h-9">
            {reminderLabel()}
          </Select.Trigger>
          <Select.Content>
            <Select.Item value="none" label={$_('calendar.composer.reminder.none')} />
            <Select.Item value="0" label={$_('calendar.composer.reminder.atTime')} />
            <Select.Item value="5" label={$_('calendar.composer.reminder.fiveMin')} />
            <Select.Item value="15" label={$_('calendar.composer.reminder.fifteenMin')} />
            <Select.Item value="30" label={$_('calendar.composer.reminder.thirtyMin')} />
            <Select.Item value="60" label={$_('calendar.composer.reminder.oneHour')} />
            <Select.Item value="1440" label={$_('calendar.composer.reminder.oneDay')} />
            <Select.Item value="custom" label={$_('calendar.composer.reminder.custom')} />
          </Select.Content>
        </Select.Root>
        {#if reminderChoice === 'custom'}
          <div class="mt-2">
            <Label for="cal-composer-reminder-custom">{$_('calendar.composer.reminder.customMinutesLabel')}</Label>
            <Input
              id="cal-composer-reminder-custom"
              type="number"
              min="0"
              bind:value={reminderCustomMinutes}
              disabled={submitting}
            />
          </div>
        {/if}
      </div>

      {#if errorMessage}
        <div class="flex items-start gap-2 p-2 bg-destructive/10 rounded text-sm">
          <Icon icon="mdi:alert-circle" class="w-4 h-4 text-destructive shrink-0 mt-0.5" />
          <div class="text-xs text-destructive break-words">{errorMessage}</div>
        </div>
      {/if}
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={close} disabled={submitting}>
        {$_('calendar.common.cancel')}
      </Button>
      <Button onclick={handleSave} disabled={submitting || localCalendars.length === 0}>
        {#if submitting}<Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />{/if}
        {$_('calendar.common.save')}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
