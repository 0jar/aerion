// Calendar sources + their calendars store. Mirrors
// extensions/contacts/frontend/stores/contactSources.svelte.ts in shape:
// lazy-load via explicit calls (not eager $effect), cache the result,
// surface loading / error state for the UI.

// @ts-ignore - wailsjs bindings
import {
  Calendar_ListSources,
  Calendar_ListCalendars,
  Calendar_AddCalDAVSource,
  Calendar_DeleteSource,
  Calendar_SyncSource,
  Calendar_SyncAllSources,
  Calendar_SetCalendarVisible,
  Calendar_SetCalendarColor,
} from '$wailsjs/go/app/App.js'
// @ts-ignore - wailsjs bindings
import type { backend } from '$wailsjs/go/models'

type Source = backend.Source
type Calendar = backend.Calendar

let sources = $state<Source[]>([])
let calendarsBySource = $state<Record<string, Calendar[]>>({})
let loading = $state(false)
let lastError = $state<string | null>(null)

// Flatten all visible calendar IDs across all sources. Used as the input
// to Calendar_ListEventsInRange so the events store knows which calendars
// to fetch occurrences for.
const visibleCalendarIDs = $derived.by(() => {
  const out: string[] = []
  for (const src of sources) {
    const cals = calendarsBySource[src.id] || []
    for (const cal of cals) {
      if (cal.visible) out.push(cal.id)
    }
  }
  return out
})

async function load() {
  loading = true
  lastError = null
  try {
    const fetched = (await Calendar_ListSources()) || []
    sources = fetched

    const next: Record<string, Calendar[]> = {}
    for (const src of fetched) {
      const cals = (await Calendar_ListCalendars(src.id)) || []
      next[src.id] = cals
    }
    calendarsBySource = next
  } catch (err) {
    lastError = (err as Error)?.message ?? String(err)
    console.error('Failed to load calendar sources:', err)
  } finally {
    loading = false
  }
}

async function addCalDAVSource(name: string, url: string, username: string, password: string): Promise<string> {
  const sourceID = await Calendar_AddCalDAVSource(name, url, username, password)
  await load()
  return sourceID
}

async function deleteSource(sourceID: string) {
  await Calendar_DeleteSource(sourceID)
  await load()
}

async function syncSource(sourceID: string) {
  await Calendar_SyncSource(sourceID)
  // No explicit reload — the `calendar:sync-complete` event the syncer
  // emits will trigger the events store to refetch its window. We DO
  // reload sources so last_synced_at + last_error update in the sidebar.
  await load()
}

async function syncAll() {
  await Calendar_SyncAllSources()
  await load()
}

async function setVisible(calendarID: string, visible: boolean) {
  await Calendar_SetCalendarVisible(calendarID, visible)
  // Optimistic local update so the UI reacts instantly without waiting
  // for a reload.
  for (const sid of Object.keys(calendarsBySource)) {
    const cals = calendarsBySource[sid]
    for (const cal of cals) {
      if (cal.id === calendarID) {
        cal.visible = visible
      }
    }
  }
  // Reassign to trigger reactivity.
  calendarsBySource = { ...calendarsBySource }
}

async function setColor(calendarID: string, hex: string) {
  await Calendar_SetCalendarColor(calendarID, hex)
  for (const sid of Object.keys(calendarsBySource)) {
    const cals = calendarsBySource[sid]
    for (const cal of cals) {
      if (cal.id === calendarID) {
        cal.color = hex
      }
    }
  }
  calendarsBySource = { ...calendarsBySource }
}

function colorOf(calendarID: string): string {
  for (const sid of Object.keys(calendarsBySource)) {
    const cals = calendarsBySource[sid]
    for (const cal of cals) {
      if (cal.id === calendarID) {
        return cal.color || defaultColor(cal.id)
      }
    }
  }
  return defaultColor(calendarID)
}

// Deterministic per-calendar default color so each calendar gets a stable
// hue even before the user customizes one. Hash the id into the hue space.
function defaultColor(calendarID: string): string {
  let hash = 0
  for (let i = 0; i < calendarID.length; i++) {
    hash = (hash * 31 + calendarID.charCodeAt(i)) | 0
  }
  const hue = Math.abs(hash) % 360
  return `hsl(${hue}, 65%, 55%)`
}

export const calendarSources = {
  get sources() { return sources },
  get calendarsBySource() { return calendarsBySource },
  get loading() { return loading },
  get lastError() { return lastError },
  get visibleCalendarIDs() { return visibleCalendarIDs },

  load,
  addCalDAVSource,
  deleteSource,
  syncSource,
  syncAll,
  setVisible,
  setColor,
  colorOf,
}
