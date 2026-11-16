/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from "react"
import { getApiBaseUrl } from "@/lib/base-url"
import {
  DEFAULT_DISPLAY_PROFILE_ENTRY,
  DEFAULT_DISPLAY_PROFILE_ID,
  newDisplayProfileEntry,
  parseDisplayProfileEntry,
  validateDisplayProfileName,
  type DisplayProfile,
  type DisplayProfileColor,
  type DisplayProfileEntry
} from "@/lib/display-profiles"

/**
 * DB-backed client settings (issue #2406).
 *
 * localStorage stays as the instant-boot cache; the server's client_settings
 * table is the source of truth. Every write goes to localStorage immediately
 * and is queued for a debounced bulk PUT. useClientSettingsSync pulls the
 * server state, applies it locally, seeds missing keys from localStorage and
 * opens the push gate.
 *
 * Values are raw strings end to end; each setting keeps the exact encoding it
 * always had in localStorage, and the backend never parses them. A cleared
 * setting stores "" instead of removing the key, so "absent on the server"
 * always means "never synced".
 */

const CHANGE_EVENT = "qui-client-setting-changed"
const FLUSH_DELAY_MS = 800

export const TORRENT_VIEW_MODE_KEYS = {
  legacy: "qui-torrent-view-mode",
  mobile: "qui-torrent-mobile-view-mode",
  desktop: "qui-torrent-desktop-view-mode",
} as const

const LEGACY_TORRENT_VIEW_MODES = {
  normal: { mobile: "normal", desktop: "normal" },
  dense: { mobile: "compact", desktop: "dense" },
  compact: { mobile: "compact", desktop: "dense" },
  "ultra-compact": { mobile: "ultra-compact", desktop: "dense" },
} as const

// Keys synced to the server. Grown as hooks convert; keys not listed here
// (theme boot caches, dismissed banners, sessionStorage) stay local-only.
const SYNCED_KEYS = new Set<string>([
  "qui-delete-files-default",
  "qui-delete-files-lock",
  "qui-speed-units",
  "qui-incognito-mode",
  "qui-datetime-preferences",
  "qui-titlebar-speeds-enabled",
  "qui.language",
  "qui-sidebar-collapsed",
  "qui-filter-sidebar-collapsed",
  "qui-accordion",
  "qui-accordion-views-seeded",
  TORRENT_VIEW_MODE_KEYS.legacy,
  TORRENT_VIEW_MODE_KEYS.mobile,
  TORRENT_VIEW_MODE_KEYS.desktop,
  "qui-unified-instance-filter",
  "torrent-details-last-tab",
  "qui-log-levels",
  "qui-theme-visibility",
  "qui-group-copies",
  "qui-statistics-series-colors",
  "qui-statistics-period",
  "qui-dashboard-chart-view",
])
const SYNCED_PREFIXES = [
  "qui-cross-seed-blocklist-",
  // "qui-filters-" covers "qui-filters-global" and the per-instance keys.
  "qui-column-visibility:",
  "qui-column-order:",
  "qui-column-sorting:",
  "qui-column-sizing:",
  "qui-stretch-name-column:",
  "qui-column-filters-",
  "qui-collapsed-categories-",
  "qui-filters-",
  "qui:torrent-mobile-sort:",
  "qui-show-empty-",
  "qui-selected-instance-",
  "qui-feature-notice-seen:",
  // The values and the entries of Display profiles other than Default.
  "qui-display-profile:",
  "qui-display-profile-entry:",
]

function isSyncedKey(key: string): boolean {
  return SYNCED_KEYS.has(key) || SYNCED_PREFIXES.some((prefix) => key.startsWith(prefix))
}

// Every key outside these two groups is a Shared setting: one value for every Display profile.
export type SettingGroup = "display" | "sortAndFilters" | "shared"

const SETTING_GROUPS: Record<Exclude<SettingGroup, "shared">, { keys: readonly string[]; prefixes: readonly string[] }> = {
  display: {
    keys: [
      TORRENT_VIEW_MODE_KEYS.mobile,
      TORRENT_VIEW_MODE_KEYS.desktop,
      "qui-sidebar-collapsed",
      "qui-filter-sidebar-collapsed",
      "qui-accordion",
      "qui-accordion-views-seeded",
    ],
    prefixes: [
      "qui-column-visibility:",
      "qui-column-order:",
      "qui-column-sizing:",
      "qui-stretch-name-column:",
      "qui-collapsed-categories-",
      "qui-show-empty-",
    ],
  },
  sortAndFilters: {
    keys: ["qui-unified-instance-filter", "qui-group-copies", "torrent-details-last-tab"],
    prefixes: [
      "qui-column-sorting:",
      "qui:torrent-mobile-sort:",
      "qui-column-filters-",
      "qui-filters-",
      "qui-selected-instance-",
    ],
  },
}

/** The group of a setting key of today (not of a profile copy). */
export function settingGroup(key: string): SettingGroup {
  for (const [group, { keys, prefixes }] of Object.entries(SETTING_GROUPS)) {
    if (keys.includes(key) || prefixes.some((prefix) => key.startsWith(prefix))) return group as SettingGroup
  }
  return "shared"
}

export function readRaw(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

// Marks a value that a tab copied from the server into the shared
// localStorage, so another tab's flush does not push it back as an edit.
// Local-only: the prefix matches no synced key.
const SERVER_COPY_PREFIX = "qui-client-settings-server-copy:"

function writeLocal(key: string, raw: string, fromServer = false): boolean {
  try {
    // Mark before value, so another tab never reads a server copy without its mark.
    if (fromServer) localStorage.setItem(SERVER_COPY_PREFIX + key, "1")
    else localStorage.removeItem(SERVER_COPY_PREFIX + key)
    localStorage.setItem(key, raw)
  } catch (error) {
    console.error("Failed to write client setting to localStorage:", error)
    return false
  }
  if (isProfileStateKey(key)) invalidateDisplayProfiles()
  window.dispatchEvent(new CustomEvent(CHANGE_EVENT, { detail: { key } }))
  return true
}

/**
 * Store a setting locally and queue it for the server. No-op when the value
 * is unchanged, which also breaks server-apply echo loops.
 */
export function writeRaw(key: string, raw: string): void {
  if (readRaw(key) === raw) return
  if (writeLocal(key, raw)) enqueuePush(key, raw)
}

// --- push queue ---

const pending = new Map<string, string>()
let flushTimer: ReturnType<typeof setTimeout> | null = null
// Opens after the first successful GET, so nothing fires while logged out.
let syncReady = false
const syncReadyListeners = new Set<() => void>()

function setSyncReady(ready: boolean): void {
  if (syncReady === ready) return
  syncReady = ready
  for (const listener of syncReadyListeners) listener()
}

function subscribeSyncReady(callback: () => void): () => void {
  syncReadyListeners.add(callback)
  return () => syncReadyListeners.delete(callback)
}

/** True once the first server snapshot is in localStorage, so a read returns the server value. */
export function useClientSettingsLoaded(): boolean {
  return useSyncExternalStore(subscribeSyncReady, () => syncReady)
}

// Set when the first read of the server failed. The push gate stays closed.
let snapshotFailed = false

/** Records that the first read of the server failed, so that pages stop waiting for it and use the local values. */
export function markClientSettingsUnavailable(): void {
  if (snapshotFailed) return
  snapshotFailed = true
  for (const listener of syncReadyListeners) listener()
}

/**
 * True once the first read of the server ended, with or without success. Before that, a setting can show the default
 * or an old local value that the read then replaces, so a pick of the value that looks selected is lost.
 */
export function useClientSettingsSettled(): boolean {
  return useSyncExternalStore(subscribeSyncReady, () => syncReady || snapshotFailed)
}

// Durable outbox: the dirty key list mirrored to localStorage. The pagehide
// keepalive PUT is not reliable (Chrome drops keepalive fetches from
// service-worker-controlled pages on unload), so unacked writes must survive
// the page. The next boot loads them back into pending, which both blocks the
// stale server snapshot from reverting them and replays their push. Only keys
// are stored; the values already live in localStorage under those keys.
// ponytail: single outbox key is last-writer-wins across tabs, so one tab's
// crash can drop another tab's unacked key; read-merge-write if that bites.
const OUTBOX_KEY = "qui-client-settings-outbox"

function persistPending(): void {
  try {
    if (pending.size === 0) localStorage.removeItem(OUTBOX_KEY)
    else localStorage.setItem(OUTBOX_KEY, JSON.stringify([...pending.keys()]))
  } catch {
    // Best effort; losing the outbox only reopens the unload race.
  }
}

function loadPersistedPending(): void {
  try {
    const raw = localStorage.getItem(OUTBOX_KEY)
    if (!raw) return
    for (const key of JSON.parse(raw) as string[]) {
      // A key gone from localStorage has no value to replay; "" would push
      // the cleared sentinel, so skip it.
      const value = readRaw(key)
      if (value !== null) pending.set(key, value)
    }
  } catch {
    // Corrupt outbox: drop it, the values themselves are still in localStorage.
  }
}

loadPersistedPending()

function enqueuePush(key: string, raw: string): void {
  pending.set(key, raw)
  persistPending()
  scheduleFlush()
}

function scheduleFlush(): void {
  if (!syncReady || pending.size === 0) return
  if (flushTimer !== null) clearTimeout(flushTimer)
  flushTimer = setTimeout(() => {
    flushTimer = null
    void flushPending()
  }, FLUSH_DELAY_MS)
}

let flushInFlight = false
// A flush needed after the current PUT, either requested while in flight or
// after another tab replaces a batch value.
let flushRequestedInFlight = false

// Chrome refuses a keepalive body over 64 KiB, so no PUT body is larger.
const MAX_PUT_BODY_BYTES = 64 * 1024
const utf8 = new TextEncoder()

/** Splits entries into PUT bodies of at most MAX_PUT_BODY_BYTES; an entry over the limit gets a body of its own. */
function splitIntoBodies(entries: [string, string][]): [string, string][][] {
  const chunks: [string, string][][] = []
  let chunk: [string, string][] = []
  let bytes = 2 // "{}"
  for (const entry of entries) {
    const entryBytes = utf8.encode(`${JSON.stringify(entry[0])}:${JSON.stringify(entry[1])}`).length
    if (chunk.length > 0 && bytes + 1 + entryBytes > MAX_PUT_BODY_BYTES) {
      chunks.push(chunk)
      chunk = []
      bytes = 2
    }
    bytes += (chunk.length > 0 ? 1 : 0) + entryBytes
    chunk.push(entry)
  }
  if (chunk.length > 0) chunks.push(chunk)
  return chunks
}

async function flushPending(atPageHide = false): Promise<void> {
  if (flushInFlight) {
    flushRequestedInFlight = true
    return
  }
  if (!syncReady || pending.size === 0) return
  // Keys stay in pending while the PUT is in flight so a concurrent server
  // apply cannot clobber the newer local value (the echo guard checks
  // pending). On failure they simply stay queued for the next flush.
  const chunks = splitIntoBodies([...pending])
  flushInFlight = true
  try {
    // One PUT at a time: Chrome counts every keepalive body in flight against the same 64 KiB.
    for (const chunk of chunks) {
      // Plain fetch instead of the api client: this module must stay import-light (i18n boots on it).
      const response = await fetch(`${getApiBaseUrl()}/client-settings`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          "X-Requested-With": "XMLHttpRequest",
        },
        body: JSON.stringify(Object.fromEntries(chunk)),
        // keepalive lets the PUT outlive the page; Chrome caps its body, so other saves leave it off.
        keepalive: atPageHide,
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      // Drop what this PUT delivered. Entries replaced in this tab stay queued;
      // a newer edit from another tab is copied from the shared localStorage.
      // A server copy is not an edit: pushing it back can be one write old, and
      // two tabs then flip the value forever (#3424).
      for (const [key, raw] of chunk) {
        const latest = readRaw(key)
        if (latest !== null && latest !== raw && readRaw(SERVER_COPY_PREFIX + key) === null) {
          pending.set(key, latest)
          flushRequestedInFlight = true
        } else if (pending.get(key) === raw) pending.delete(key)
      }
      persistPending()
    }
  } catch (error) {
    // No self-retry, on purpose: a failed batch stays pending until the next write
    // or tab hide. After logout the push gate stays open, and those PUTs fail harmlessly.
    console.error("Failed to push client settings:", error)
  } finally {
    flushInFlight = false
    if (flushRequestedInFlight) {
      flushRequestedInFlight = false
      scheduleFlush()
    }
  }
}

function flushAtPageHide(): void {
  if (flushTimer !== null) {
    clearTimeout(flushTimer)
    flushTimer = null
  }
  void flushPending(true)
}

if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flushAtPageHide()
  })
  // Chrome does not fire visibilitychange on a same-tab reload or navigation,
  // so a write inside the debounce window would die with the page and the
  // stale server snapshot would revert it at next boot. keepalive lets the
  // PUT outlive the page.
  window.addEventListener("pagehide", flushAtPageHide)
}

// --- Display profiles ---
//
// Default keeps the keys of today. Another profile keeps the copy of a Display setting under
// PROFILE_VALUE_PREFIX + id + ":" + key, and its entry under PROFILE_ENTRY_PREFIX + id. The pick of
// this browser is local only and never synced.

const PROFILE_VALUE_PREFIX = "qui-display-profile:"
const PROFILE_ENTRY_PREFIX = "qui-display-profile-entry:"
const ACTIVE_PROFILE_KEY = "qui-display-profile-active"

function isProfileStateKey(key: string): boolean {
  return key === ACTIVE_PROFILE_KEY || key.startsWith(PROFILE_ENTRY_PREFIX)
}

function profileValueKey(profileId: string, key: string): string {
  return `${PROFILE_VALUE_PREFIX}${profileId}:${key}`
}

function profileEntryKey(profileId: string): string {
  return PROFILE_ENTRY_PREFIX + profileId
}

export interface DisplayProfilesState {
  /** Default first, then the other profiles by name. */
  profiles: readonly DisplayProfile[]
  /** The profile of this browser. Default when the browser never picked one, or when its pick has no entry. */
  active: DisplayProfile
}

let profilesState: DisplayProfilesState | null = null

function invalidateDisplayProfiles(): void {
  profilesState = null
}

if (typeof window !== "undefined") {
  // Registered before any hook subscribes, so a hook that re-reads after a storage event sees the new profiles.
  window.addEventListener("storage", (event) => {
    if (event.key == null || isProfileStateKey(event.key)) invalidateDisplayProfiles()
  })
}

function loadDisplayProfiles(): DisplayProfilesState {
  let defaultEntry = DEFAULT_DISPLAY_PROFILE_ENTRY
  const others: DisplayProfile[] = []
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (!key?.startsWith(PROFILE_ENTRY_PREFIX)) continue
      const id = key.slice(PROFILE_ENTRY_PREFIX.length)
      const entry = parseDisplayProfileEntry(localStorage.getItem(key) ?? "")
      if (id === DEFAULT_DISPLAY_PROFILE_ID) defaultEntry = entry ?? DEFAULT_DISPLAY_PROFILE_ENTRY
      else if (entry) others.push({ ...entry, id, isDefault: false })
    }
  } catch (error) {
    console.error("Failed to read Display profiles:", error)
  }
  others.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) || a.id.localeCompare(b.id))
  const profiles = [{ ...defaultEntry, ownSortAndFilters: false, id: DEFAULT_DISPLAY_PROFILE_ID, isDefault: true }, ...others]
  const activeId = readRaw(ACTIVE_PROFILE_KEY)
  return { profiles, active: profiles.find((profile) => profile.id === activeId) ?? profiles[0] }
}

/** The profiles and the profile of this browser. The object stays the same until a profile or the pick changes. */
export function getDisplayProfiles(): DisplayProfilesState {
  profilesState ??= loadDisplayProfiles()
  return profilesState
}

/** True when the profile keeps its own copy of the setting instead of the key of today. */
function keepsOwnCopy(profile: DisplayProfile, key: string): boolean {
  const group = settingGroup(key)
  return group === "display" || (group === "sortAndFilters" && profile.ownSortAndFilters)
}

/** The storage key that holds a setting for the profile of this browser. */
function resolveKey(key: string): string {
  const { active } = getDisplayProfiles()
  if (active.isDefault || !keepsOwnCopy(active, key)) return key
  return profileValueKey(active.id, key)
}

/** Makes this browser use the profile. Only this browser remembers the pick. Every live setting re-reads at once. */
export function pickDisplayProfile(profileId: string): void {
  writeLocal(ACTIVE_PROFILE_KEY, profileId)
}

function randomProfileId(): string {
  // getRandomValues, not randomUUID: randomUUID needs a secure context, and qui often runs on plain http.
  const bytes = crypto.getRandomValues(new Uint8Array(12))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
}

export type DisplayProfileLook = Partial<Pick<DisplayProfileEntry, "icon" | "color">>

/**
 * Makes a profile from the Display settings of the active profile, for every instance, and makes this browser use
 * it. The icon and the color come from look, not from the source; with no look the icon is bookmark and the color
 * None. Throws when validateDisplayProfileName rejects the name. Returns the id of the new profile.
 */
export function createDisplayProfile(name: string, look: DisplayProfileLook = {}): string {
  const { profiles, active } = getDisplayProfiles()
  const error = validateDisplayProfileName(name, profiles)
  if (error) throw new Error(`invalid Display profile name: ${error}`)

  const sourcePrefix = active.isDefault ? "" : profileValueKey(active.id, "")
  const copies: [string, string][] = []
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const storageKey = localStorage.key(i)
      if (!storageKey?.startsWith(sourcePrefix)) continue
      const key = storageKey.slice(sourcePrefix.length)
      const raw = localStorage.getItem(storageKey)
      if (raw !== null && keepsOwnCopy(active, key)) copies.push([key, raw])
    }
  } catch (scanError) {
    console.error("Failed to copy the Display settings of the active profile:", scanError)
  }

  const id = randomProfileId()
  for (const [key, raw] of copies) writeRaw(profileValueKey(id, key), raw)
  writeRaw(profileEntryKey(id), JSON.stringify({
    ...newDisplayProfileEntry(name.trim()),
    ...look,
    sameColumns: active.sameColumns,
    ownSortAndFilters: active.ownSortAndFilters,
  }))
  pickDisplayProfile(id)
  return id
}

/** Renames a profile for every browser. Default can be renamed too. Throws when validateDisplayProfileName rejects the name. */
export function renameDisplayProfile(profileId: string, name: string): void {
  const { profiles } = getDisplayProfiles()
  const profile = profiles.find((candidate) => candidate.id === profileId)
  if (!profile) throw new Error(`unknown Display profile: ${profileId}`)
  const error = validateDisplayProfileName(name, profiles, profileId)
  if (error) throw new Error(`invalid Display profile name: ${error}`)
  const { id: _id, isDefault: _isDefault, ...entry } = profile
  writeRaw(profileEntryKey(profileId), JSON.stringify({ ...entry, name: name.trim() }))
}

/** The storage keys that hold the values of a profile other than Default. */
function profileValueKeys(profileId: string): string[] {
  const prefix = profileValueKey(profileId, "")
  const keys: string[] = []
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key?.startsWith(prefix)) keys.push(key)
    }
  } catch (error) {
    console.error("Failed to read the values of a Display profile:", error)
  }
  return keys
}

/**
 * Deletes a profile for every browser: its entry and its values become "". A browser that uses it moves to Default.
 * Throws for Default.
 */
export function deleteDisplayProfile(profileId: string): void {
  if (profileId === DEFAULT_DISPLAY_PROFILE_ID) throw new Error("the Default Display profile cannot be deleted")
  const wasActive = readRaw(ACTIVE_PROFILE_KEY) === profileId
  // The entry first, so another browser on this profile moves to Default before it reads blank values.
  writeRaw(profileEntryKey(profileId), "")
  for (const key of profileValueKeys(profileId)) writeRaw(key, "")
  if (wasActive) pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
}

function updateDisplayProfileEntry(profileId: string, change: Partial<DisplayProfileEntry>): void {
  const profile = getDisplayProfiles().profiles.find((candidate) => candidate.id === profileId)
  if (!profile) return
  const { id: _id, isDefault: _isDefault, ...entry } = profile
  writeRaw(profileEntryKey(profileId), JSON.stringify({ ...entry, ...change }))
}

/** Saves the lucide icon name of a profile. */
export function setDisplayProfileIcon(profileId: string, icon: string): void {
  updateDisplayProfileEntry(profileId, { icon })
}

/** Saves the color of a profile. null is the color None. */
export function setDisplayProfileColor(profileId: string, color: DisplayProfileColor | null): void {
  updateDisplayProfileEntry(profileId, { color })
}

/** The Sort and filters values stored under the prefix, as pairs of the key of today and the raw value. */
function storedSortAndFilters(prefix: string): [string, string][] {
  const values: [string, string][] = []
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const storageKey = localStorage.key(i)
      if (!storageKey?.startsWith(prefix)) continue
      const key = storageKey.slice(prefix.length)
      const raw = localStorage.getItem(storageKey)
      if (raw !== null && settingGroup(key) === "sortAndFilters") values.push([key, raw])
    }
  } catch (error) {
    console.error("Failed to read the Sort and filters values:", error)
  }
  return values
}

/**
 * Sets "Own sort and filters" of a profile. On copies the shared values to the profile, so nothing changes at that
 * moment. Off writes "" to the own values, and the profile reads the shared values again. Throws for Default, which
 * always shares.
 */
export function setDisplayProfileOwnSortAndFilters(profileId: string, on: boolean): void {
  const profile = getDisplayProfiles().profiles.find((candidate) => candidate.id === profileId)
  if (!profile || profile.isDefault) throw new Error(`Display profile ${profileId} cannot keep its own sort and filters`)
  if (profile.ownSortAndFilters === on) return
  const { name, icon, color, sameColumns } = profile
  const entry = JSON.stringify({ name, icon, color, sameColumns, ownSortAndFilters: on } satisfies DisplayProfileEntry)

  // The entry flips last on the way in and first on the way out, so the profile never reads blank own values.
  if (on) {
    for (const [key, raw] of storedSortAndFilters("")) writeRaw(profileValueKey(profileId, key), raw)
    writeRaw(profileEntryKey(profileId), entry)
  } else {
    writeRaw(profileEntryKey(profileId), entry)
    for (const [key] of storedSortAndFilters(profileValueKey(profileId, ""))) writeRaw(profileValueKey(profileId, key), "")
  }
}

function subscribeDisplayProfiles(callback: () => void): () => void {
  const onStorage = (event: StorageEvent) => {
    if (event.key == null || isProfileStateKey(event.key)) callback()
  }
  const onChange = (event: Event) => {
    const key = (event as CustomEvent<{ key?: string }>).detail?.key
    if (key && isProfileStateKey(key)) callback()
  }
  window.addEventListener("storage", onStorage)
  window.addEventListener(CHANGE_EVENT, onChange)
  return () => {
    window.removeEventListener("storage", onStorage)
    window.removeEventListener(CHANGE_EVENT, onChange)
  }
}

export interface DisplayProfilesApi extends DisplayProfilesState {
  pick: (profileId: string) => void
  create: (name: string, look?: DisplayProfileLook) => string
  rename: (profileId: string, name: string) => void
  delete: (profileId: string) => void
  setIcon: (profileId: string, icon: string) => void
  setColor: (profileId: string, color: DisplayProfileColor | null) => void
  setOwnSortAndFilters: (profileId: string, on: boolean) => void
}

/** The Display profiles for the UI. */
export function useDisplayProfiles(): DisplayProfilesApi {
  const state = useSyncExternalStore(subscribeDisplayProfiles, getDisplayProfiles)
  return useMemo(() => ({
    ...state,
    pick: pickDisplayProfile,
    create: createDisplayProfile,
    rename: renameDisplayProfile,
    delete: deleteDisplayProfile,
    setIcon: setDisplayProfileIcon,
    setColor: setDisplayProfileColor,
    setOwnSortAndFilters: setDisplayProfileOwnSortAndFilters,
  }), [state])
}

// --- server sync (called by useClientSettingsSync) ---

const activeProfileDeletedListeners = new Set<(profile: DisplayProfile) => void>()

/**
 * Calls back when a server snapshot deletes the profile of this browser, after the browser moved to Default. The
 * listener gets the deleted profile. Returns the unsubscribe.
 */
export function onActiveDisplayProfileDeleted(listener: (profile: DisplayProfile) => void): () => void {
  activeProfileDeletedListeners.add(listener)
  return () => activeProfileDeletedListeners.delete(listener)
}

function deletedProfileIds(settings: Record<string, string>): string[] {
  return Object.entries(settings)
    .filter(([key, raw]) => raw === "" && key.startsWith(PROFILE_ENTRY_PREFIX))
    .map(([key]) => key.slice(PROFILE_ENTRY_PREFIX.length))
    .filter((id) => id !== DEFAULT_DISPLAY_PROFILE_ID)
}

function isKeyOfProfile(key: string, profileId: string): boolean {
  return key === profileEntryKey(profileId) || key.startsWith(profileValueKey(profileId, ""))
}

function isValueOfDeletedProfile(key: string): boolean {
  if (!key.startsWith(PROFILE_VALUE_PREFIX)) return false
  const profileId = key.slice(PROFILE_VALUE_PREFIX.length).split(":", 1)[0]
  return readRaw(profileEntryKey(profileId)) === ""
}

function dropPendingOfProfile(profileId: string): void {
  let dropped = false
  for (const key of pending.keys()) {
    if (!isKeyOfProfile(key, profileId)) continue
    pending.delete(key)
    dropped = true
  }
  if (dropped) persistPending()
}

/**
 * Apply a server snapshot to localStorage. Keys with a pending local push and
 * keys already equal are skipped. Returns the keys that changed.
 */
export function applyServerSettings(settings: Record<string, string>): string[] {
  const { active } = getDisplayProfiles()
  const deletedIds = deletedProfileIds(settings)
  // A delete wins over the unsaved writes of this browser, so they cannot bring the profile back.
  for (const id of deletedIds) dropPendingOfProfile(id)

  const changed: string[] = []
  for (const [key, raw] of Object.entries(settings)) {
    if (pending.has(key)) continue
    if (readRaw(key) === raw) continue
    if (writeLocal(key, raw, true)) changed.push(key)
  }

  if (!active.isDefault && deletedIds.includes(active.id)) {
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    for (const listener of activeProfileDeletedListeners) listener(active)
  }
  return changed
}

/**
 * Queue every synced localStorage key the server does not know yet, then open
 * the push gate. Idempotent: after one push the server has the key.
 */
export function seedAndMarkReady(serverSettings: Record<string, string>): void {
  const legacyMode = readRaw(TORRENT_VIEW_MODE_KEYS.legacy)
  const migratedModes = legacyMode
    ? LEGACY_TORRENT_VIEW_MODES[legacyMode as keyof typeof LEGACY_TORRENT_VIEW_MODES]
    : undefined
  if (migratedModes) {
    if (readRaw(TORRENT_VIEW_MODE_KEYS.mobile) === null) {
      writeRaw(TORRENT_VIEW_MODE_KEYS.mobile, migratedModes.mobile)
    }
    if (readRaw(TORRENT_VIEW_MODE_KEYS.desktop) === null) {
      writeRaw(TORRENT_VIEW_MODE_KEYS.desktop, migratedModes.desktop)
    }
  }

  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (!key || !isSyncedKey(key) || key in serverSettings || pending.has(key) || isValueOfDeletedProfile(key)) continue
      const raw = localStorage.getItem(key)
      if (raw !== null) pending.set(key, raw)
    }
  } catch (error) {
    console.error("Failed to scan localStorage for client settings seed:", error)
  }
  setSyncReady(true)
  scheduleFlush()
}

// Test-only: reset module state between vitest cases. Mirrors a fresh page
// boot: in-memory state resets, then the persisted outbox loads back in.
export function _resetClientSettingsForTests(): void {
  pending.clear()
  if (flushTimer !== null) clearTimeout(flushTimer)
  flushTimer = null
  flushInFlight = false
  flushRequestedInFlight = false
  snapshotFailed = false
  setSyncReady(false)
  invalidateDisplayProfiles()
  loadPersistedPending()
}

// --- React hook ---

/** JSON-parse a raw value, accepting only a real boolean. */
export function parseJsonBoolean(raw: string): boolean {
  const parsed = JSON.parse(raw)
  if (typeof parsed !== "boolean") throw new Error("not a boolean")
  return parsed
}

// Module-level so the setter's useCallback identity stays stable when no
// custom serializer is passed.
const defaultSerialize = (value: unknown): string => JSON.stringify(value)

/**
 * Drop a pre-instance-scoping shared localStorage key once the caller uses
 * instance-scoped keys. Local-only: legacy keys are never synced.
 */
export function useDropLegacyKey(baseKey: string, enabled: boolean): void {
  useEffect(() => {
    if (!enabled) return
    try {
      localStorage.removeItem(baseKey)
    } catch (error) {
      console.error("Failed to clear legacy state key:", baseKey, error)
    }
  }, [baseKey, enabled])
}

/**
 * Moves the value of a local-only legacy key into a setting of the active profile, when the setting has no value and
 * isValid accepts the legacy value. Drops the legacy key either way.
 */
export function migrateLegacyLocalValue(legacyKey: string, key: string, isValid: (raw: string) => boolean): void {
  try {
    const legacy = localStorage.getItem(legacyKey)
    if (legacy && readRaw(resolveKey(key)) == null && isValid(legacy)) writeRaw(resolveKey(key), legacy)
    localStorage.removeItem(legacyKey)
  } catch (error) {
    console.error("Failed to migrate legacy state key:", legacyKey, error)
  }
}

interface ClientSettingOptions<T> {
  defaultValue: T
  /** Raw string to value; throw to fall back to defaultValue. Pass a stable function. */
  parse: (raw: string) => T
  /** Value to raw string; defaults to JSON.stringify. Pass a stable function. */
  serialize?: (value: T) => string
}

function decode<T>(raw: string | null, { defaultValue, parse }: ClientSettingOptions<T>): T {
  // "" is the cleared sentinel (settings never remove their key).
  if (raw == null || raw === "") return defaultValue
  try {
    return parse(raw)
  } catch {
    return defaultValue
  }
}

/** The current value of one setting, for code outside a component. */
export function readClientSetting<T>(key: string, options: ClientSettingOptions<T>): T {
  return decode(readRaw(resolveKey(key)), options)
}

/** Stores one setting and queues it for the server, for code outside a component. The setter of useClientSetting calls it too. */
export function writeClientSetting<T>(key: string, value: T, serialize: (value: T) => string = defaultSerialize): void {
  writeRaw(resolveKey(key), serialize(value))
}

/** Calls back after any setting changes, in this tab or another one. Returns the unsubscribe. */
export function subscribeClientSettings(callback: () => void): () => void {
  window.addEventListener("storage", callback)
  window.addEventListener(CHANGE_EVENT, callback)
  return () => {
    window.removeEventListener("storage", callback)
    window.removeEventListener(CHANGE_EVENT, callback)
  }
}

/**
 * One DB-backed setting as React state. All hook instances for a key stay in
 * sync in the same tab (change event) and across tabs (storage event); writes
 * persist to localStorage and queue a debounced server push.
 */
export function useClientSetting<T>(
  key: string,
  { defaultValue, parse, serialize = defaultSerialize }: ClientSettingOptions<T>
): [T, (value: T | ((prev: T) => T)) => void] {
  const subscribe = useCallback(
    (callback: () => void) => {
      // A storage event without a key is localStorage.clear() (or a test's
      // bare Event); re-read for those too.
      // A profile change can move the key to another storage key, so it re-reads too.
      const onStorage = (e: StorageEvent) => {
        if (e.key == null || e.key === resolveKey(key) || isProfileStateKey(e.key)) callback()
      }
      const onChange = (e: Event) => {
        const changed = (e as CustomEvent<{ key?: string }>).detail?.key
        if (changed && (changed === resolveKey(key) || isProfileStateKey(changed))) callback()
      }
      window.addEventListener("storage", onStorage)
      window.addEventListener(CHANGE_EVENT, onChange)
      return () => {
        window.removeEventListener("storage", onStorage)
        window.removeEventListener(CHANGE_EVENT, onChange)
      }
    },
    [key]
  )

  const raw = useSyncExternalStore(subscribe, () => readRaw(resolveKey(key)))

  const value = useMemo(() => decode(raw, { defaultValue, parse }), [raw, parse, defaultValue])

  const valueRef = useRef(value)
  valueRef.current = value

  // valueRef changes only on render, so two functional updates of one key in the
  // same tick both read the old value and the last one wins. Nothing writes on
  // mount: corrupt stored JSON renders as the default until the first edit.
  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      const resolved = typeof next === "function" ? (next as (prev: T) => T)(valueRef.current) : next
      writeClientSetting(key, resolved, serialize)
    },
    [key, serialize]
  )

  return [value, set]
}
