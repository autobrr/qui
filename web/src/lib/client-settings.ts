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

export const FILTER_SIDEBAR_COLLAPSED_KEY = "qui-filter-sidebar-collapsed"

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
  "qui-sidebar-icon-only",
  "qui-sidebar-hidden",
  FILTER_SIDEBAR_COLLAPSED_KEY,
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
  "qui-dashboard-open-instances",
  "qui-support-shortcut-hidden",
  "qui-support-reminder-off",
  "qui-support-reminder-due",
  "qui-problem-reports-seen-at",
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
  // The values, the entries, and the tombstones of Display profiles.
  "qui-display-profile:",
  "qui-display-profile-entry:",
  "qui-display-profile-deleted:",
]

function isSyncedKey(key: string): boolean {
  return SYNCED_KEYS.has(key) || SYNCED_PREFIXES.some((prefix) => key.startsWith(prefix))
}

// The keys of a Column setup. Each ends in ":" and the instance id (0 is the Unified view), or in
// ":" + SHARED_COLUMN_SETUP for the one setup that every instance reads when Same columns is on.
const COLUMN_SETUP_PREFIXES = ["qui-column-visibility:", "qui-column-order:", "qui-column-sizing:", "qui-stretch-name-column:"]
const SHARED_COLUMN_SETUP = "every-instance"

// Every key outside these two groups is a Shared setting: one value for every Display profile.
export type SettingGroup = "display" | "sortAndFilters" | "shared"

const SETTING_GROUPS: Record<Exclude<SettingGroup, "shared">, { keys: readonly string[]; prefixes: readonly string[] }> = {
  display: {
    keys: [
      TORRENT_VIEW_MODE_KEYS.mobile,
      TORRENT_VIEW_MODE_KEYS.desktop,
      "qui-sidebar-icon-only",
      "qui-sidebar-hidden",
      FILTER_SIDEBAR_COLLAPSED_KEY,
      "qui-accordion",
      "qui-accordion-views-seeded",
      "qui-dashboard-open-instances",
    ],
    prefixes: [...COLUMN_SETUP_PREFIXES, "qui-collapsed-categories-", "qui-show-empty-"],
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

/** The prefix of a Column setup key of one instance, or undefined for any other key. */
function instanceColumnSetupPrefix(key: string): string | undefined {
  const prefix = COLUMN_SETUP_PREFIXES.find((candidate) => key.startsWith(candidate))
  if (prefix === undefined || key.length === prefix.length || key.slice(prefix.length) === SHARED_COLUMN_SETUP) return undefined
  return prefix
}

// A few hundred keys at most, and resolveKey asks for each one on every read.
const settingGroups = new Map<string, SettingGroup>()

/** The group of a setting key of today (not of a profile copy). */
export function settingGroup(key: string): SettingGroup {
  let group = settingGroups.get(key)
  if (group === undefined) {
    group = (Object.keys(SETTING_GROUPS) as Exclude<SettingGroup, "shared">[]).find((candidate) => {
      const { keys, prefixes } = SETTING_GROUPS[candidate]
      return keys.includes(key) || prefixes.some((prefix) => key.startsWith(prefix))
    }) ?? "shared"
    settingGroups.set(key, group)
  }
  return group
}

export function readRaw(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

/** Every key in localStorage. A read that fails ends the list. */
function storedKeys(): string[] {
  const keys: string[] = []
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key !== null) keys.push(key)
    }
  } catch (error) {
    console.error("Failed to read the keys of localStorage:", error)
  }
  return keys
}

// Marks a value that a tab copied from the server into the shared
// localStorage, so another tab's flush does not push it back as an edit.
// Local-only: the prefix matches no synced key.
const SERVER_COPY_PREFIX = "qui-client-settings-server-copy:"

function writeLocal(key: string, raw: string, fromServer = false): boolean {
  const profilesChange = changesDisplayProfiles(key)
  try {
    // Mark before value, so another tab never reads a server copy without its mark.
    if (fromServer) localStorage.setItem(SERVER_COPY_PREFIX + key, "1")
    else localStorage.removeItem(SERVER_COPY_PREFIX + key)
    localStorage.setItem(key, raw)
  } catch (error) {
    console.error("Failed to write client setting to localStorage:", error)
    return false
  }
  if (profilesChange) displayProfilesChanged()
  window.dispatchEvent(new CustomEvent(CHANGE_EVENT, { detail: { key } }))
  return true
}

/**
 * Calls back after a write of a key that matches, in this tab or another one, and after localStorage.clear() in
 * another tab. Returns the unsubscribe.
 */
function subscribeToKeys(callback: () => void, matches: (key: string) => boolean): () => void {
  const onStorage = (event: StorageEvent) => {
    if (event.key == null || matches(event.key)) callback()
  }
  const onChange = (event: Event) => {
    const key = (event as CustomEvent<{ key?: string }>).detail?.key
    if (key && matches(key)) callback()
  }
  window.addEventListener("storage", onStorage)
  window.addEventListener(CHANGE_EVENT, onChange)
  return () => {
    window.removeEventListener("storage", onStorage)
    window.removeEventListener(CHANGE_EVENT, onChange)
  }
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
// Grows each time a key is queued. A PUT drops a key only when nothing queued it again after the PUT read it.
const queuedGeneration = new Map<string, number>()
let generation = 0

function queue(key: string, raw: string): void {
  pending.set(key, raw)
  queuedGeneration.set(key, ++generation)
}

function unqueue(key: string): void {
  pending.delete(key)
  queuedGeneration.delete(key)
}
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
      if (value !== null) queue(key, value)
    }
  } catch {
    // Corrupt outbox: drop it, the values themselves are still in localStorage.
  }
}

loadPersistedPending()

function enqueuePush(key: string, raw: string): void {
  queue(key, raw)
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
let keepaliveInFlight = false
// A flush needed after the current PUT, either requested while in flight or
// after another tab replaces a batch value.
let flushRequestedInFlight = false
// PUTs in flight: page hide can start one beside a debounced one.
let putsInFlight = 0

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

/**
 * Sends the pending entries in PUT bodies of at most MAX_PUT_BODY_BYTES, and drops each entry that a PUT delivered.
 * Keys stay in pending while their PUT is in flight, so a concurrent server apply cannot clobber the newer local value
 * (the echo guard checks pending). Throws at the first failed PUT; the rest stays queued for the next flush.
 */
async function putPending(keepalive: boolean): Promise<void> {
  if (keepalive) keepaliveInFlight = true
  putsInFlight++
  try {
    const sentGeneration = new Map(queuedGeneration)
    // One PUT at a time: Chrome counts every keepalive body in flight against the same 64 KiB.
    for (const chunk of splitIntoBodies([...pending])) {
      // Plain fetch instead of the api client: this module must stay import-light (i18n boots on it).
      const response = await fetch(`${getApiBaseUrl()}/client-settings`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          "X-Requested-With": "XMLHttpRequest",
        },
        body: JSON.stringify(Object.fromEntries(chunk)),
        // keepalive lets the PUT outlive the page; Chrome caps its body, so other saves leave it off.
        keepalive,
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      // Drop what this PUT delivered. Entries replaced in this tab stay queued;
      // a newer edit from another tab is copied from the shared localStorage.
      // A server copy is not an edit: pushing it back can be one write old, and
      // two tabs then flip the value forever (#3424).
      for (const [key, raw] of chunk) {
        const latest = readRaw(key)
        if (latest !== null && latest !== raw && readRaw(SERVER_COPY_PREFIX + key) === null) {
          queue(key, latest)
          flushRequestedInFlight = true
        } else if (pending.get(key) === raw && queuedGeneration.get(key) === sentGeneration.get(key)) {
          // The other PUT can land an older value after this one, so the key stays queued and goes out once more.
          if (putsInFlight > 1) flushRequestedInFlight = true
          else unqueue(key)
        }
      }
      persistPending()
    }
  } finally {
    putsInFlight--
    if (keepalive) keepaliveInFlight = false
  }
}

// No self-retry, on purpose: a failed batch stays pending until the next write
// or tab hide. After logout the push gate stays open, and those PUTs fail harmlessly.
function logPushError(error: unknown): void {
  console.error("Failed to push client settings:", error)
}

async function flushPending(atPageHide = false): Promise<void> {
  if (flushInFlight) {
    flushRequestedInFlight = true
    // The page can cancel the PUT in flight, which has no keepalive. A PUT sets values, so sending them twice is safe.
    if (atPageHide && !keepaliveInFlight) void putPending(true).catch(logPushError).finally(flushIfRequested)
    return
  }
  if (!syncReady || pending.size === 0) return
  flushInFlight = true
  try {
    await putPending(atPageHide)
  } catch (error) {
    logPushError(error)
  } finally {
    flushInFlight = false
    flushIfRequested()
  }
}

/** Schedules the flush that a finished PUT asked for, unless a flush is in flight and does it at its end. */
function flushIfRequested(): void {
  if (flushInFlight || !flushRequestedInFlight) return
  flushRequestedInFlight = false
  scheduleFlush()
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

// Display profiles: Default keeps the keys of today. Another profile keeps the copy of a Display setting under
// PROFILE_VALUE_PREFIX + id + ":" + key, and its entry under PROFILE_ENTRY_PREFIX + id. The pick of
// this browser is local only and never synced.

const PROFILE_VALUE_PREFIX = "qui-display-profile:"
const PROFILE_ENTRY_PREFIX = "qui-display-profile-entry:"
// No edit writes a tombstone, so an edit from a browser that missed the delete cannot bring the profile back.
const PROFILE_TOMBSTONE_PREFIX = "qui-display-profile-deleted:"
const ACTIVE_PROFILE_KEY = "qui-display-profile-active"
// The last instance that a torrent table of this browser showed. Local only, like the pick.
const LAST_INSTANCE_KEY = "qui-display-profile-last-instance"

function isProfileStateKey(key: string): boolean {
  return key === ACTIVE_PROFILE_KEY || key.startsWith(PROFILE_ENTRY_PREFIX) || key.startsWith(PROFILE_TOMBSTONE_PREFIX)
}

function profileValueKey(profileId: string, key: string): string {
  return `${PROFILE_VALUE_PREFIX}${profileId}:${key}`
}

function profileEntryKey(profileId: string): string {
  return PROFILE_ENTRY_PREFIX + profileId
}

function profileTombstoneKey(profileId: string): string {
  return PROFILE_TOMBSTONE_PREFIX + profileId
}

/** The storage key of a setting of today in a profile. Default keeps the key of today. */
function profileStorageKey(profile: DisplayProfile, key: string): string {
  return profile.isDefault ? key : profileValueKey(profile.id, key)
}

/** The profile and the key prefix of a stored Column setup of one instance, or null for any other storage key. */
function instanceColumnSetupOwner(storageKey: string): { profileId: string; prefix: string } | null {
  let profileId = DEFAULT_DISPLAY_PROFILE_ID
  let key = storageKey
  if (storageKey.startsWith(PROFILE_VALUE_PREFIX)) {
    const rest = storageKey.slice(PROFILE_VALUE_PREFIX.length)
    const separator = rest.indexOf(":")
    if (separator <= 0) return null
    profileId = rest.slice(0, separator)
    key = rest.slice(separator + 1)
  }
  const prefix = instanceColumnSetupPrefix(key)
  return prefix === undefined ? null : { profileId, prefix }
}

export interface DisplayProfilesState {
  /** Default first, then the other profiles by name. */
  profiles: readonly DisplayProfile[]
  /** The profile of this browser. Default when the browser never picked one, or when its pick has no entry. */
  active: DisplayProfile
}

let profilesState: DisplayProfilesState | null = null
// The profile of this tab at the last read, so a storage event can show that another tab applied its delete.
let lastActiveProfile: DisplayProfile | null = null
const displayProfileListeners = new Set<() => void>()

function invalidateDisplayProfiles(): void {
  profilesState = null
}

function displayProfilesChanged(): void {
  invalidateDisplayProfiles()
  for (const listener of displayProfileListeners) listener()
}

/**
 * True when a write of the key can change the profiles: the pick, an entry, a tombstone, or the first Column setup of
 * an instance in a profile whose Same columns is computed. Call it before the write.
 */
function changesDisplayProfiles(key: string): boolean {
  if (isProfileStateKey(key)) return true
  const owner = instanceColumnSetupOwner(key)
  if (owner === null || profilesState === null) return false
  const profile = findDisplayProfile(owner.profileId, profilesState)
  return profile !== undefined && profile.sameColumns === null && profile.usesSameColumns
}

function loadDisplayProfiles(): DisplayProfilesState {
  let defaultEntry = DEFAULT_DISPLAY_PROFILE_ENTRY
  const others: (DisplayProfileEntry & { id: string })[] = []
  const withColumnSetup = new Set<string>()
  const deleted = new Set<string>()
  for (const key of storedKeys()) {
    if (key.startsWith(PROFILE_TOMBSTONE_PREFIX)) {
      deleted.add(key.slice(PROFILE_TOMBSTONE_PREFIX.length))
      continue
    }
    if (!key.startsWith(PROFILE_ENTRY_PREFIX)) {
      const owner = instanceColumnSetupOwner(key)
      if (owner !== null) withColumnSetup.add(owner.profileId)
      continue
    }
    const id = key.slice(PROFILE_ENTRY_PREFIX.length)
    const entry = parseDisplayProfileEntry(readRaw(key) ?? "")
    if (id === DEFAULT_DISPLAY_PROFILE_ID) defaultEntry = entry ?? DEFAULT_DISPLAY_PROFILE_ENTRY
    else if (entry) others.push({ ...entry, id })
  }
  const listed = others
    .filter((entry) => !deleted.has(entry.id))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) || a.id.localeCompare(b.id))
  const toProfile = (entry: DisplayProfileEntry, id: string, isDefault: boolean): DisplayProfile => ({
    ...entry,
    id,
    isDefault,
    usesSameColumns: entry.sameColumns ?? !withColumnSetup.has(id),
  })
  const profiles = [
    toProfile({ ...defaultEntry, ownSortAndFilters: false }, DEFAULT_DISPLAY_PROFILE_ID, true),
    ...listed.map((entry) => toProfile(entry, entry.id, false)),
  ]
  const activeId = readRaw(ACTIVE_PROFILE_KEY)
  return { profiles, active: profiles.find((profile) => profile.id === activeId) ?? profiles[0] }
}

/** The profiles and the profile of this browser. The object stays the same until a profile or the pick changes. */
export function getDisplayProfiles(): DisplayProfilesState {
  if (profilesState === null) {
    profilesState = loadDisplayProfiles()
    lastActiveProfile = profilesState.active
  }
  return profilesState
}

function findDisplayProfile(profileId: string, { profiles } = getDisplayProfiles()): DisplayProfile | undefined {
  return profiles.find((profile) => profile.id === profileId)
}

/** True when the profile keeps its own copy of the setting instead of the key of today. */
function keepsOwnCopy(profile: DisplayProfile, key: string): boolean {
  const group = settingGroup(key)
  return group === "display" || (group === "sortAndFilters" && profile.ownSortAndFilters)
}

/** The storage key that a write of a setting goes to, by default for the profile of this browser. */
function resolveKey(key: string, profile: DisplayProfile = getDisplayProfiles().active): string {
  if (!keepsOwnCopy(profile, key)) return key
  const prefix = profile.usesSameColumns ? instanceColumnSetupPrefix(key) : undefined
  return profileStorageKey(profile, prefix === undefined ? key : prefix + SHARED_COLUMN_SETUP)
}

/**
 * The storage key that a read of a setting comes from. It differs from resolveKey only for an instance that has no
 * Column setup of its own while Same columns is off: that instance reads the shared setup, the last one it showed.
 */
function resolveReadKey(key: string, profile: DisplayProfile = getDisplayProfiles().active): string {
  const storageKey = resolveKey(key, profile)
  const prefix = instanceColumnSetupPrefix(key)
  if (prefix === undefined || readRaw(storageKey) !== null) return storageKey
  return profileStorageKey(profile, prefix + SHARED_COLUMN_SETUP)
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
  for (const storageKey of storedKeys()) {
    if (!storageKey.startsWith(sourcePrefix)) continue
    const key = storageKey.slice(sourcePrefix.length)
    const raw = readRaw(storageKey)
    if (raw !== null && keepsOwnCopy(active, key)) copies.push([key, raw])
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
  const profile = findDisplayProfile(profileId)
  if (!profile) throw new Error(`unknown Display profile: ${profileId}`)
  const error = validateDisplayProfileName(name, profiles, profileId)
  if (error) throw new Error(`invalid Display profile name: ${error}`)
  updateDisplayProfileEntry(profile, { name: name.trim() })
}

/**
 * Deletes a profile for every browser: its entry and its values become "". A browser that uses it moves to Default.
 * Throws for Default.
 */
export function deleteDisplayProfile(profileId: string): void {
  if (profileId === DEFAULT_DISPLAY_PROFILE_ID) throw new Error("the Default Display profile cannot be deleted")
  const wasActive = readRaw(ACTIVE_PROFILE_KEY) === profileId
  // The tombstone and the entry first, so another browser on this profile moves to Default before it reads blank values.
  writeRaw(profileTombstoneKey(profileId), "1")
  writeRaw(profileEntryKey(profileId), "")
  const valuePrefix = profileValueKey(profileId, "")
  for (const key of storedKeys()) if (key.startsWith(valuePrefix)) writeRaw(key, "")
  if (wasActive) pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
}

/**
 * Writes the change over the stored entry of the profile. The fields that this version does not know stay, and the
 * entry of Default starts from the defaults while it has none.
 */
function updateDisplayProfileEntry(profile: DisplayProfile, change: Partial<DisplayProfileEntry>): void {
  const key = profileEntryKey(profile.id)
  const raw = readRaw(key) ?? ""
  const stored: object = parseDisplayProfileEntry(raw) ? JSON.parse(raw) : DEFAULT_DISPLAY_PROFILE_ENTRY
  writeRaw(key, JSON.stringify({ ...stored, ...change }))
}

export function setDisplayProfileIcon(profileId: string, icon: string): void {
  const profile = findDisplayProfile(profileId)
  if (profile) updateDisplayProfileEntry(profile, { icon })
}

/** Saves the color of a profile. null is the color None. */
export function setDisplayProfileColor(profileId: string, color: DisplayProfileColor | null): void {
  const profile = findDisplayProfile(profileId)
  if (profile) updateDisplayProfileEntry(profile, { color })
}

function readLastInstance(): number | null {
  const raw = readRaw(LAST_INSTANCE_KEY)
  const id = raw ? Number(raw) : Number.NaN
  return Number.isInteger(id) ? id : null
}

/** Records the instance that a torrent table shows (0 is the Unified view), so Settings can name and copy it. */
export function recordColumnSetupInstance(instanceId: number): void {
  if (readLastInstance() !== instanceId) writeLocal(LAST_INSTANCE_KEY, String(instanceId))
}

function subscribeLastInstance(callback: () => void): () => void {
  return subscribeToKeys(callback, (key) => key === LAST_INSTANCE_KEY)
}

/** The last instance that a torrent table of this browser showed, or null when none did. */
export function useLastColumnSetupInstance(): number | null {
  return useSyncExternalStore(subscribeLastInstance, readLastInstance)
}

/**
 * Sets Same columns on every instance for a profile. Turning it on copies the Column setup of instanceId (by default
 * the last instance that this browser showed) to the shared setup. Turning it off copies the shared setup to every
 * instance, so no instance changes on screen.
 */
export function setDisplayProfileSameColumns(profileId: string, on: boolean, instanceId: number | null = readLastInstance()): void {
  const profile = findDisplayProfile(profileId)
  if (!profile) return
  if (on && !profile.usesSameColumns && instanceId !== null) {
    for (const prefix of COLUMN_SETUP_PREFIXES) {
      const own = readRaw(profileStorageKey(profile, prefix + instanceId))
      if (own !== null) writeRaw(profileStorageKey(profile, prefix + SHARED_COLUMN_SETUP), own)
    }
  }
  if (!on && profile.usesSameColumns) {
    // An instance with no setup of its own already reads the shared setup.
    for (const storageKey of storedKeys()) {
      const owner = instanceColumnSetupOwner(storageKey)
      if (owner?.profileId === profile.id) writeRaw(storageKey, readRaw(profileStorageKey(profile, owner.prefix + SHARED_COLUMN_SETUP)) ?? "")
    }
  }
  updateDisplayProfileEntry(profile, { sameColumns: on })
}

/** The Sort and filters values stored under the prefix, as pairs of the key of today and the raw value. */
function storedSortAndFilters(prefix: string): [string, string][] {
  const values: [string, string][] = []
  for (const storageKey of storedKeys()) {
    if (!storageKey.startsWith(prefix)) continue
    const key = storageKey.slice(prefix.length)
    const raw = readRaw(storageKey)
    if (raw !== null && settingGroup(key) === "sortAndFilters") values.push([key, raw])
  }
  return values
}

/**
 * Sets "Own sort and filters" of a profile. On copies the shared values to the profile, so nothing changes at that
 * moment. Off writes "" to the own values, and the profile reads the shared values again. Throws for Default, which
 * always shares.
 */
export function setDisplayProfileOwnSortAndFilters(profileId: string, on: boolean): void {
  const profile = findDisplayProfile(profileId)
  if (!profile || profile.isDefault) throw new Error(`Display profile ${profileId} cannot keep its own sort and filters`)
  if (profile.ownSortAndFilters === on) return

  // The entry flips last on the way in and first on the way out, so the profile never reads blank own values.
  if (on) {
    for (const [key, raw] of storedSortAndFilters("")) writeRaw(profileValueKey(profileId, key), raw)
    updateDisplayProfileEntry(profile, { ownSortAndFilters: true })
  } else {
    updateDisplayProfileEntry(profile, { ownSortAndFilters: false })
    for (const [key] of storedSortAndFilters(profileValueKey(profileId, ""))) writeRaw(profileValueKey(profileId, key), "")
  }
}

function subscribeDisplayProfiles(callback: () => void): () => void {
  displayProfileListeners.add(callback)
  return () => displayProfileListeners.delete(callback)
}

export interface DisplayProfilesApi extends DisplayProfilesState {
  /**
   * False until the first read of the server ends. Until then the actions can copy old local values over newer
   * values of the server, so the UI keeps them disabled.
   */
  settled: boolean
  pick: (profileId: string) => void
  create: (name: string, look?: DisplayProfileLook) => string
  rename: (profileId: string, name: string) => void
  delete: (profileId: string) => void
  setIcon: (profileId: string, icon: string) => void
  setColor: (profileId: string, color: DisplayProfileColor | null) => void
  setSameColumns: (profileId: string, on: boolean, instanceId?: number | null) => void
  setOwnSortAndFilters: (profileId: string, on: boolean) => void
}

export function useDisplayProfiles(): DisplayProfilesApi {
  const state = useSyncExternalStore(subscribeDisplayProfiles, getDisplayProfiles)
  const settled = useClientSettingsSettled()
  return useMemo(() => ({
    ...state,
    settled,
    pick: pickDisplayProfile,
    create: createDisplayProfile,
    rename: renameDisplayProfile,
    delete: deleteDisplayProfile,
    setIcon: setDisplayProfileIcon,
    setColor: setDisplayProfileColor,
    setSameColumns: setDisplayProfileSameColumns,
    setOwnSortAndFilters: setDisplayProfileOwnSortAndFilters,
  }), [state, settled])
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
  const ids = new Set<string>()
  for (const [key, raw] of Object.entries(settings)) {
    if (key.startsWith(PROFILE_TOMBSTONE_PREFIX)) ids.add(key.slice(PROFILE_TOMBSTONE_PREFIX.length))
    else if (raw === "" && key.startsWith(PROFILE_ENTRY_PREFIX)) ids.add(key.slice(PROFILE_ENTRY_PREFIX.length))
  }
  ids.delete(DEFAULT_DISPLAY_PROFILE_ID)
  return [...ids]
}

function isKeyOfProfile(key: string, profileId: string): boolean {
  return key === profileEntryKey(profileId) || key.startsWith(profileValueKey(profileId, ""))
}

function isProfileDeleted(profileId: string): boolean {
  return readRaw(profileTombstoneKey(profileId)) !== null || readRaw(profileEntryKey(profileId)) === ""
}

function isValueOfDeletedProfile(key: string): boolean {
  if (!key.startsWith(PROFILE_VALUE_PREFIX)) return false
  return isProfileDeleted(key.slice(PROFILE_VALUE_PREFIX.length).split(":", 1)[0])
}

function notifyActiveProfileDeleted(profile: DisplayProfile): void {
  for (const listener of activeProfileDeletedListeners) listener(profile)
}

if (typeof window !== "undefined") {
  // Registered before any hook subscribes, so a hook that re-reads after a storage event sees the new profiles.
  window.addEventListener("storage", (event) => {
    if (event.key != null && !changesDisplayProfiles(event.key)) return
    const before = lastActiveProfile
    displayProfilesChanged()
    // Another tab of this browser applied a server delete of the profile that this tab uses.
    if (event.key === null || !before || before.isDefault || readRaw(SERVER_COPY_PREFIX + event.key) === null) return
    if (getDisplayProfiles().active.id !== before.id && isProfileDeleted(before.id)) notifyActiveProfileDeleted(before)
  })
}

function dropPendingOfProfile(profileId: string): void {
  let dropped = false
  for (const key of pending.keys()) {
    if (!isKeyOfProfile(key, profileId)) continue
    unqueue(key)
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
    notifyActiveProfileDeleted(active)
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

  for (const key of storedKeys()) {
    if (!isSyncedKey(key) || key in serverSettings || pending.has(key) || isValueOfDeletedProfile(key)) continue
    const raw = readRaw(key)
    if (raw !== null) queue(key, raw)
  }
  setSyncReady(true)
  scheduleFlush()
}

// Test-only: reset module state between vitest cases. Mirrors a fresh page
// boot: in-memory state resets, then the persisted outbox loads back in.
export function _resetClientSettingsForTests(): void {
  pending.clear()
  queuedGeneration.clear()
  if (flushTimer !== null) clearTimeout(flushTimer)
  flushTimer = null
  flushInFlight = false
  keepaliveInFlight = false
  flushRequestedInFlight = false
  putsInFlight = 0
  snapshotFailed = false
  setSyncReady(false)
  invalidateDisplayProfiles()
  lastActiveProfile = null
  loadPersistedPending()
}

// --- React hook ---

/** JSON-parse a raw value, accepting only a real boolean. */
export function parseJsonBoolean(raw: string): boolean {
  const parsed = JSON.parse(raw)
  if (typeof parsed !== "boolean") throw new Error("not a boolean")
  return parsed
}

export function parseJsonNumber(raw: string): number {
  const parsed: unknown = JSON.parse(raw)
  if (typeof parsed !== "number") throw new Error("not a number")
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
 * isValid accepts the legacy value. Drops the legacy key, except when the storage refuses the move.
 */
export function migrateLegacyLocalValue(legacyKey: string, key: string, isValid: (raw: string) => boolean): void {
  try {
    const legacy = localStorage.getItem(legacyKey)
    if (legacy && readRaw(resolveReadKey(key)) == null && isValid(legacy)) {
      const target = resolveKey(key)
      writeRaw(target, legacy)
      if (readRaw(target) !== legacy) return
    }
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
  return decode(readRaw(resolveReadKey(key)), options)
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
 * The value of one setting as a profile sees it, whatever profile this browser uses, for a place that shows
 * profiles side by side, such as Settings. A profile that does not exist reads the default.
 */
export function useDisplayProfileSetting<T>(profileId: string, key: string, { defaultValue, parse }: ClientSettingOptions<T>): T {
  const raw = useSyncExternalStore(subscribeClientSettings, () => {
    const profile = findDisplayProfile(profileId)
    return profile ? readRaw(resolveReadKey(key, profile)) : null
  })
  return useMemo(() => decode(raw, { defaultValue, parse }), [raw, parse, defaultValue])
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
      const unsubscribeKey = subscribeToKeys(callback, (changed) => changed === resolveKey(key) || changed === resolveReadKey(key))
      // A profile change can move the key to another storage key.
      const unsubscribeProfiles = subscribeDisplayProfiles(callback)
      return () => {
        unsubscribeKey()
        unsubscribeProfiles()
      }
    },
    [key]
  )

  const raw = useSyncExternalStore(subscribe, () => readRaw(resolveReadKey(key)))

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
