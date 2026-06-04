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

// sourceTypeOf returns the type of the source that owns this calendar
// ('caldav' or 'local'), or '' if not found. EventDetail uses it to gate
// the Edit/Delete buttons to local-source events only (CalDAV writes are
// Phase 2).
function sourceTypeOf(calendarID: string): string {
  for (const src of sources) {
    const cals = calendarsBySource[src.id] || []
    for (const cal of cals) {
      if (cal.id === calendarID) return src.type
    }
  }
  return ''
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

// Hex-string equivalents for places that need #rrggbb specifically — namely
// the kit ColorPicker, which validates input as 7-char hex and falls back to
// its first preset (blue) when given an HSL string. Same hash → same color
// as defaultColor() / colorOf().
function colorOfHex(calendarID: string): string {
  for (const sid of Object.keys(calendarsBySource)) {
    const cals = calendarsBySource[sid]
    for (const cal of cals) {
      if (cal.id === calendarID) {
        if (cal.color && cal.color !== '') return cal.color
        return defaultColorHex(cal.id)
      }
    }
  }
  return defaultColorHex(calendarID)
}

function defaultColorHex(calendarID: string): string {
  let hash = 0
  for (let i = 0; i < calendarID.length; i++) {
    hash = (hash * 31 + calendarID.charCodeAt(i)) | 0
  }
  const hue = Math.abs(hash) % 360
  return hslToHex(hue, 65, 55)
}

// Standard HSL → hex conversion. Saturation + lightness are percentages
// (0..100). Returns a 7-char #rrggbb string suitable for ColorPicker's
// `value` prop.
function hslToHex(h: number, s: number, l: number): string {
  const sn = s / 100
  const ln = l / 100
  const c = (1 - Math.abs(2 * ln - 1)) * sn
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1))
  const m = ln - c / 2
  let r = 0, g = 0, b = 0
  if (h < 60)        { r = c; g = x; b = 0 }
  else if (h < 120)  { r = x; g = c; b = 0 }
  else if (h < 180)  { r = 0; g = c; b = x }
  else if (h < 240)  { r = 0; g = x; b = c }
  else if (h < 300)  { r = x; g = 0; b = c }
  else               { r = c; g = 0; b = x }
  const to2 = (v: number) => Math.round((v + m) * 255).toString(16).padStart(2, '0')
  return `#${to2(r)}${to2(g)}${to2(b)}`
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
  colorOfHex,
  sourceTypeOf,
}
