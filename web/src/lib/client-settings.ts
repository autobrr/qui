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
  "qui-search-type",
])
const SYNCED_PREFIXES = [
  "qui-cross-seed-blocklist-",
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
  "qui-display-profile:",
  "qui-display-profile-entry:",
  "qui-display-profile-deleted:",
]

function isSyncedKey(key: string): boolean {
  return SYNCED_KEYS.has(key) || SYNCED_PREFIXES.some((prefix) => key.startsWith(prefix))
}

const COLUMN_SETUP_PREFIXES = ["qui-column-visibility:", "qui-column-order:", "qui-column-sizing:", "qui-stretch-name-column:"]
const SHARED_COLUMN_SETUP = "every-instance"

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

function instanceColumnSetupPrefix(key: string): string | undefined {
  const prefix = COLUMN_SETUP_PREFIXES.find((candidate) => key.startsWith(candidate))
  if (prefix === undefined || key.length === prefix.length || key.slice(prefix.length) === SHARED_COLUMN_SETUP) return undefined
  return prefix
}

const settingGroups = new Map<string, SettingGroup>()

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

const SERVER_COPY_PREFIX = "qui-client-settings-server-copy:"

function writeLocal(key: string, raw: string, fromServer = false): boolean {
  const profilesChange = changesDisplayProfiles(key)
  try {
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

export function writeRaw(key: string, raw: string): void {
  if (readRaw(key) === raw) return
  if (writeLocal(key, raw)) enqueuePush(key, raw)
}

const pending = new Map<string, string>()
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

export function useClientSettingsLoaded(): boolean {
  return useSyncExternalStore(subscribeSyncReady, () => syncReady)
}

let snapshotFailed = false

export function markClientSettingsUnavailable(): void {
  if (snapshotFailed) return
  snapshotFailed = true
  for (const listener of syncReadyListeners) listener()
}

export function useClientSettingsSettled(): boolean {
  return useSyncExternalStore(subscribeSyncReady, () => syncReady || snapshotFailed)
}

const OUTBOX_KEY = "qui-client-settings-outbox"

function persistPending(): void {
  try {
    if (pending.size === 0) localStorage.removeItem(OUTBOX_KEY)
    else localStorage.setItem(OUTBOX_KEY, JSON.stringify([...pending.keys()]))
  } catch {
    return
  }
}

function loadPersistedPending(): void {
  try {
    const raw = localStorage.getItem(OUTBOX_KEY)
    if (!raw) return
    for (const key of JSON.parse(raw) as string[]) {
      const value = readRaw(key)
      if (value !== null) queue(key, value)
    }
  } catch {
    return
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
let flushRequestedInFlight = false
let putsInFlight = 0

const MAX_PUT_BODY_BYTES = 64 * 1024
const utf8 = new TextEncoder()

function splitIntoBodies(entries: [string, string][]): [string, string][][] {
  const chunks: [string, string][][] = []
  let chunk: [string, string][] = []
  let bytes = 2
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

async function putPending(keepalive: boolean): Promise<void> {
  if (keepalive) keepaliveInFlight = true
  putsInFlight++
  try {
    const sentGeneration = new Map(queuedGeneration)
    for (const chunk of splitIntoBodies([...pending])) {
      const response = await fetch(`${getApiBaseUrl()}/client-settings`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          "X-Requested-With": "XMLHttpRequest",
        },
        body: JSON.stringify(Object.fromEntries(chunk)),
        keepalive,
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      for (const [key, raw] of chunk) {
        const latest = readRaw(key)
        if (latest !== null && latest !== raw && readRaw(SERVER_COPY_PREFIX + key) === null) {
          queue(key, latest)
          flushRequestedInFlight = true
        } else if (pending.get(key) === raw && queuedGeneration.get(key) === sentGeneration.get(key)) {
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

function logPushError(error: unknown): void {
  console.error("Failed to push client settings:", error)
}

async function flushPending(atPageHide = false): Promise<void> {
  if (flushInFlight) {
    flushRequestedInFlight = true
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
  window.addEventListener("pagehide", flushAtPageHide)
}

const PROFILE_VALUE_PREFIX = "qui-display-profile:"
const PROFILE_ENTRY_PREFIX = "qui-display-profile-entry:"
const PROFILE_TOMBSTONE_PREFIX = "qui-display-profile-deleted:"
const ACTIVE_PROFILE_KEY = "qui-display-profile-active"
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

function profileStorageKey(profile: DisplayProfile, key: string): string {
  return profile.isDefault ? key : profileValueKey(profile.id, key)
}

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
  profiles: readonly DisplayProfile[]
  active: DisplayProfile
}

let profilesState: DisplayProfilesState | null = null
let lastActiveProfile: DisplayProfile | null = null
const displayProfileListeners = new Set<() => void>()

function invalidateDisplayProfiles(): void {
  profilesState = null
}

function displayProfilesChanged(): void {
  invalidateDisplayProfiles()
  for (const listener of displayProfileListeners) listener()
}

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

function keepsOwnCopy(profile: DisplayProfile, key: string): boolean {
  const group = settingGroup(key)
  return group === "display" || (group === "sortAndFilters" && profile.ownSortAndFilters)
}

function resolveKey(key: string, profile: DisplayProfile = getDisplayProfiles().active): string {
  if (!keepsOwnCopy(profile, key)) return key
  const prefix = profile.usesSameColumns ? instanceColumnSetupPrefix(key) : undefined
  return profileStorageKey(profile, prefix === undefined ? key : prefix + SHARED_COLUMN_SETUP)
}

function resolveReadKey(key: string, profile: DisplayProfile = getDisplayProfiles().active): string {
  const storageKey = resolveKey(key, profile)
  const prefix = instanceColumnSetupPrefix(key)
  if (prefix === undefined || readRaw(storageKey) !== null) return storageKey
  return profileStorageKey(profile, prefix + SHARED_COLUMN_SETUP)
}

export function pickDisplayProfile(profileId: string): void {
  writeLocal(ACTIVE_PROFILE_KEY, profileId)
}

function randomProfileId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(12))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
}

export type DisplayProfileLook = Partial<Pick<DisplayProfileEntry, "icon" | "color">>

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

export function renameDisplayProfile(profileId: string, name: string): void {
  const { profiles } = getDisplayProfiles()
  const profile = findDisplayProfile(profileId)
  if (!profile) throw new Error(`unknown Display profile: ${profileId}`)
  const error = validateDisplayProfileName(name, profiles, profileId)
  if (error) throw new Error(`invalid Display profile name: ${error}`)
  updateDisplayProfileEntry(profile, { name: name.trim() })
}

export function deleteDisplayProfile(profileId: string): void {
  if (profileId === DEFAULT_DISPLAY_PROFILE_ID) throw new Error("the Default Display profile cannot be deleted")
  const wasActive = readRaw(ACTIVE_PROFILE_KEY) === profileId
  writeRaw(profileTombstoneKey(profileId), "1")
  writeRaw(profileEntryKey(profileId), "")
  const valuePrefix = profileValueKey(profileId, "")
  for (const key of storedKeys()) if (key.startsWith(valuePrefix)) writeRaw(key, "")
  if (wasActive) pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
}

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

export function setDisplayProfileColor(profileId: string, color: DisplayProfileColor | null): void {
  const profile = findDisplayProfile(profileId)
  if (profile) updateDisplayProfileEntry(profile, { color })
}

function readLastInstance(): number | null {
  const raw = readRaw(LAST_INSTANCE_KEY)
  const id = raw ? Number(raw) : Number.NaN
  return Number.isInteger(id) ? id : null
}

export function recordColumnSetupInstance(instanceId: number): void {
  if (readLastInstance() !== instanceId) writeLocal(LAST_INSTANCE_KEY, String(instanceId))
}

function subscribeLastInstance(callback: () => void): () => void {
  return subscribeToKeys(callback, (key) => key === LAST_INSTANCE_KEY)
}

export function useLastColumnSetupInstance(): number | null {
  return useSyncExternalStore(subscribeLastInstance, readLastInstance)
}

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
    for (const storageKey of storedKeys()) {
      const owner = instanceColumnSetupOwner(storageKey)
      if (owner?.profileId === profile.id) writeRaw(storageKey, readRaw(profileStorageKey(profile, owner.prefix + SHARED_COLUMN_SETUP)) ?? "")
    }
  }
  updateDisplayProfileEntry(profile, { sameColumns: on })
}

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

export function setDisplayProfileOwnSortAndFilters(profileId: string, on: boolean): void {
  const profile = findDisplayProfile(profileId)
  if (!profile || profile.isDefault) throw new Error(`Display profile ${profileId} cannot keep its own sort and filters`)
  if (profile.ownSortAndFilters === on) return

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

const activeProfileDeletedListeners = new Set<(profile: DisplayProfile) => void>()

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
  window.addEventListener("storage", (event) => {
    if (event.key != null && !changesDisplayProfiles(event.key)) return
    const before = lastActiveProfile
    displayProfilesChanged()
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

export function applyServerSettings(settings: Record<string, string>): string[] {
  const { active } = getDisplayProfiles()
  const deletedIds = deletedProfileIds(settings)
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

const defaultSerialize = (value: unknown): string => JSON.stringify(value)

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
  parse: (raw: string) => T
  serialize?: (value: T) => string
}

function decode<T>(raw: string | null, { defaultValue, parse }: ClientSettingOptions<T>): T {
  if (raw == null || raw === "") return defaultValue
  try {
    return parse(raw)
  } catch {
    return defaultValue
  }
}

export function readClientSetting<T>(key: string, options: ClientSettingOptions<T>): T {
  return decode(readRaw(resolveReadKey(key)), options)
}

export function writeClientSetting<T>(key: string, value: T, serialize: (value: T) => string = defaultSerialize): void {
  writeRaw(resolveKey(key), serialize(value))
}

export function subscribeClientSettings(callback: () => void): () => void {
  window.addEventListener("storage", callback)
  window.addEventListener(CHANGE_EVENT, callback)
  return () => {
    window.removeEventListener("storage", callback)
    window.removeEventListener(CHANGE_EVENT, callback)
  }
}

export function useDisplayProfileSetting<T>(profileId: string, key: string, { defaultValue, parse }: ClientSettingOptions<T>): T {
  const raw = useSyncExternalStore(subscribeClientSettings, () => {
    const profile = findDisplayProfile(profileId)
    return profile ? readRaw(resolveReadKey(key, profile)) : null
  })
  return useMemo(() => decode(raw, { defaultValue, parse }), [raw, parse, defaultValue])
}

export function useClientSetting<T>(
  key: string,
  { defaultValue, parse, serialize = defaultSerialize }: ClientSettingOptions<T>
): [T, (value: T | ((prev: T) => T)) => void] {
  const subscribe = useCallback(
    (callback: () => void) => {
      const unsubscribeKey = subscribeToKeys(callback, (changed) => changed === resolveKey(key) || changed === resolveReadKey(key))
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

  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      const resolved = typeof next === "function" ? (next as (prev: T) => T)(valueRef.current) : next
      writeClientSetting(key, resolved, serialize)
    },
    [key, serialize]
  )

  return [value, set]
}
