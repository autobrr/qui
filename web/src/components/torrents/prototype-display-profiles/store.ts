/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway), v2: the Display profile picker on /instances/$id
// (?variant=C by default, A and B through the switcher; ?notice=1 forces the
// Feature notice) and the Settings › General › Display profiles section.
// All state lives in this module; nothing writes Client settings. The table
// reads its visible columns and Name stretch from here, so "Same columns on
// every instance" shows on the real table. Column order and widths stay real.

import type { ColumnVisibilityState } from "@tanstack/react-table"
import { useCallback, useEffect, useSyncExternalStore, type SetStateAction } from "react"
import { toast } from "sonner"

import { DEFAULT_COLUMN_VISIBILITY } from "@/lib/torrent-table/default-columns"

export const PROTOTYPE_ENABLED = import.meta.env.DEV

export type VariantKey = "A" | "B" | "C"

export const VARIANTS: { key: VariantKey; name: string }[] = [
  { key: "A", name: "Toolbar button" },
  { key: "B", name: "Inside Toggle columns" },
  { key: "C", name: "Status bar chip" },
]

export const NAME_MAX = 32

/** The demo's instances, plus the Unified view, which counts as one more instance. */
export const PROTO_INSTANCES: { id: number; name: string }[] = [
  { id: 1, name: "seedbox" },
  { id: 2, name: "home" },
  { id: 0, name: "Unified view" },
]

export function instanceLabel(id: number): string {
  return PROTO_INSTANCES.find((i) => i.id === id)?.name ?? `instance ${id}`
}

export type RowDensity = "Table" | "Dense" | "Stacked"

/** One Column setup. Real ones also hold order and widths; the prototype leaves those to the table. */
export interface ColumnSetup {
  visibility: ColumnVisibilityState
  stretchName: boolean
}

/** Stub values: the real ones are the per-instance sort and column filters. */
export interface SortAndFilters {
  sort: string
  filters: string
}

export interface DisplayProfile {
  id: string
  name: string
  isDefault: boolean
  /** null: shares sort and filters with every profile. Always null on Default. */
  ownSortAndFilters: SortAndFilters | null
  /** undefined: not set yet, so the value is computed (see sameColumns). */
  sameColumnsSet?: boolean
  sharedColumns: ColumnSetup
  /** Stored per-instance setups, keyed by instance id (0 is the Unified view). */
  perInstanceColumns: Record<number, ColumnSetup>
  rowDensity: RowDensity
  sidebars: { filters: "open" | "closed"; details: "open" | "closed" }
  openFilterSections: string[]
  collapsedCategoryTrees: string[]
  showEmptyFilters: boolean
  copiedFrom?: string
}

interface PrototypeState {
  variant: VariantKey
  noticeForced: boolean
  noticeSeen: boolean
  noticeSeenBy: string
  pickerOpen: boolean
  /** The instance the torrent page shows; null off the torrent page. */
  currentInstanceId: number | null
  /** The last instance this browser showed. Settings turns "Same columns" on from its setup. */
  lastInstanceId: number
  /** This browser's pick. Stored in localStorage in the real feature; absent means Default. */
  activeProfileId: string
  profiles: DisplayProfile[]
  sharedSortAndFilters: SortAndFilters
  lastAction: string
}

function visibility(hidden: string[], shown: string[] = []): ColumnVisibilityState {
  const out: ColumnVisibilityState = { ...DEFAULT_COLUMN_VISIBILITY }
  for (const id of hidden) out[id] = false
  for (const id of shown) out[id] = true
  return out
}

const LAPTOP_HIDDEN = ["priority", "total_size", "num_seeds", "num_leechs", "eta", "popularity", "tags", "added_on", "status_icon"]
const PHONE_HIDDEN = [...LAPTOP_HIDDEN, "size", "dlspeed", "upspeed", "category", "tracker_icon"]

function initialProfiles(): DisplayProfile[] {
  return [
    {
      id: "default",
      name: "Default",
      isDefault: true,
      ownSortAndFilters: null,
      sharedColumns: { visibility: visibility([]), stretchName: true },
      perInstanceColumns: {
        1: { visibility: visibility([]), stretchName: true },
        2: { visibility: visibility(["popularity", "num_seeds", "num_leechs"], ["tracker", "seeding_time"]), stretchName: false },
        0: { visibility: visibility(["tags", "popularity"]), stretchName: true },
      },
      rowDensity: "Table",
      sidebars: { filters: "open", details: "open" },
      openFilterSections: ["Status", "Categories", "Tags", "Trackers"],
      collapsedCategoryTrees: [],
      showEmptyFilters: true,
    },
    {
      id: "laptop",
      name: "Laptop",
      isDefault: false,
      ownSortAndFilters: null,
      sharedColumns: { visibility: visibility(LAPTOP_HIDDEN), stretchName: true },
      perInstanceColumns: {},
      rowDensity: "Dense",
      sidebars: { filters: "closed", details: "open" },
      openFilterSections: ["Status"],
      collapsedCategoryTrees: ["movies"],
      showEmptyFilters: false,
    },
    {
      id: "phone",
      name: "Phone",
      isDefault: false,
      ownSortAndFilters: { sort: "Upload speed ↓", filters: "Status: Active" },
      sharedColumns: { visibility: visibility(PHONE_HIDDEN), stretchName: false },
      perInstanceColumns: {},
      rowDensity: "Stacked",
      sidebars: { filters: "closed", details: "closed" },
      openFilterSections: [],
      collapsedCategoryTrees: ["movies", "tv"],
      showEmptyFilters: false,
    },
  ]
}

function readParams(): Pick<PrototypeState, "variant" | "noticeForced"> {
  const params = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search)
  const raw = params.get("variant")?.toUpperCase()
  const variant = VARIANTS.some((v) => v.key === raw) ? (raw as VariantKey) : "C"
  return { variant, noticeForced: params.get("notice") === "1" }
}

function initialState(): PrototypeState {
  const params = readParams()
  return {
    ...params,
    noticeSeen: !params.noticeForced,
    noticeSeenBy: params.noticeForced ? "" : "a second profile exists",
    pickerOpen: false,
    currentInstanceId: null,
    lastInstanceId: PROTO_INSTANCES[0].id,
    activeProfileId: "laptop",
    profiles: initialProfiles(),
    sharedSortAndFilters: { sort: "Added on ↓", filters: "none" },
    lastAction: "page load: this browser picked Laptop earlier",
  }
}

let state: PrototypeState = initialState()
const listeners = new Set<() => void>()

function set(patch: Partial<PrototypeState>) {
  state = { ...state, ...patch }
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function usePrototype<T>(select: (s: PrototypeState) => T): T {
  return useSyncExternalStore(subscribe, () => select(state))
}

/** Every lookup falls back to Default, so a stale id (after a delete or an HMR swap) never yields undefined. */
function profileById(s: PrototypeState, id: string): DisplayProfile {
  const found = s.profiles.find((p) => p.id === id) ?? s.profiles.find((p) => p.isDefault)
  if (!found) throw new Error("prototype: the Default profile is missing")
  return found
}

function activeProfile(s: PrototypeState = state): DisplayProfile {
  return profileById(s, s.activeProfileId)
}

export function useActiveProfile(): DisplayProfile {
  return usePrototype(activeProfile)
}

export function useNoticeVisible(): boolean {
  return usePrototype((s) => !s.noticeSeen && !s.pickerOpen && (s.noticeForced || s.profiles.length < 2))
}

function nameOf(id: string) {
  return profileById(state, id).name
}

function updateProfile(id: string, patch: (p: DisplayProfile) => DisplayProfile): DisplayProfile[] {
  return state.profiles.map((p) => (p.id === id ? patch(p) : p))
}

/** On when the switch was never set and the profile has no stored per-instance setup. */
export function sameColumns(profile: DisplayProfile): boolean {
  return profile.sameColumnsSet ?? Object.keys(profile.perInstanceColumns).length === 0
}

export function sameColumnsIsComputed(profile: DisplayProfile): boolean {
  return profile.sameColumnsSet === undefined
}

/** The Column setup an instance reads, and where it comes from. */
export function columnsFor(profile: DisplayProfile, instanceId: number): { setup: ColumnSetup; source: "shared" | "own" | "shared copy" } {
  if (sameColumns(profile)) return { setup: profile.sharedColumns, source: "shared" }
  const own = profile.perInstanceColumns[instanceId]
  return own ? { setup: own, source: "own" } : { setup: profile.sharedColumns, source: "shared copy" }
}

/** Columns a person sees on that instance; the Instance column shows only in the Unified view. */
export function visibleColumnCount(setup: ColumnSetup, instanceId: number): number {
  return Object.entries(setup.visibility).filter(([id, on]) => on && id !== "select" && (instanceId === 0 || id !== "instance")).length
}

/** Returns an error message, or null when the name can be used. */
export function validateName(name: string, exceptId?: string): string | null {
  const trimmed = name.trim()
  if (!trimmed) return "Enter a name."
  if (trimmed.length > NAME_MAX) return `Use ${NAME_MAX} characters or fewer (now ${trimmed.length}).`
  const clash = state.profiles.find((p) => p.id !== exceptId && p.name.toLowerCase() === trimmed.toLowerCase())
  if (clash) return `${clash.name} already uses this name.`
  return null
}

/** Name stretch for the row summary: one value when every instance agrees. */
function stretchLabel(profile: DisplayProfile): string | null {
  if (sameColumns(profile)) return profile.sharedColumns.stretchName ? "Name stretch" : null
  const values = PROTO_INSTANCES.map((i) => columnsFor(profile, i.id).setup.stretchName)
  if (values.every(Boolean)) return "Name stretch"
  if (values.some(Boolean)) return "Name stretch on some instances"
  return null
}

export function summaryParts(profile: DisplayProfile, opts: { stretch?: boolean } = {}): string[] {
  const parts = [sameColumns(profile) ? "Same columns on every instance" : "Columns per instance", profile.rowDensity]
  const stretch = opts.stretch === false ? null : stretchLabel(profile)
  if (stretch) parts.push(stretch)
  parts.push(profile.isDefault || !profile.ownSortAndFilters ? "shares sort and filters" : "own sort and filters")
  return parts
}

export function summarize(profile: DisplayProfile, opts?: { stretch?: boolean }): string {
  return summaryParts(profile, opts).join(" · ")
}

function writeColumns(profile: DisplayProfile, instanceId: number, next: ColumnSetup): DisplayProfile {
  if (sameColumns(profile)) return { ...profile, sharedColumns: next }
  return { ...profile, perInstanceColumns: { ...profile.perInstanceColumns, [instanceId]: next } }
}

/**
 * Replaces usePersistedColumnVisibility and usePersistedStretchMode on the
 * torrent table while the prototype runs.
 */
export function usePrototypeColumns(instanceId: number) {
  useEffect(() => {
    set({ currentInstanceId: instanceId, lastInstanceId: instanceId })
    return () => {
      if (state.currentInstanceId === instanceId) set({ currentInstanceId: null })
    }
  }, [instanceId])

  const setup = usePrototype((s) => columnsFor(activeProfile(s), instanceId).setup)

  const setVisibility = useCallback((updater: SetStateAction<ColumnVisibilityState>) => {
    const profile = activeProfile()
    const current = columnsFor(profile, instanceId).setup
    const next = typeof updater === "function" ? updater(current.visibility) : updater
    if (JSON.stringify(next) === JSON.stringify(current.visibility)) return
    const changed = Object.keys(next).filter((id) => next[id] !== current.visibility[id])
    set({
      profiles: updateProfile(profile.id, (p) => writeColumns(p, instanceId, { ...current, visibility: next })),
      lastAction: `${profile.name}: column ${changed.join(", ")} ${changed.length === 1 && next[changed[0]] ? "shown" : "changed"} on ${sameColumns(profile) ? "every instance" : `${instanceLabel(instanceId)} only`}`,
    })
  }, [instanceId])

  const toggleStretch = useCallback(() => {
    const profile = activeProfile()
    const current = columnsFor(profile, instanceId).setup
    set({
      profiles: updateProfile(profile.id, (p) => writeColumns(p, instanceId, { ...current, stretchName: !current.stretchName })),
      lastAction: `${profile.name}: Name stretch ${current.stretchName ? "off" : "on"} on ${sameColumns(profile) ? "every instance" : `${instanceLabel(instanceId)} only`}`,
    })
  }, [instanceId])

  return {
    visibility: [setup.visibility, setVisibility] as const,
    stretch: [setup.stretchName, toggleStretch] as const,
  }
}

export const actions = {
  setVariant(variant: VariantKey) {
    const url = new URL(window.location.href)
    url.searchParams.set("variant", variant)
    window.history.replaceState(window.history.state, "", url)
    set({ variant, pickerOpen: false, lastAction: `variant → ${variant}` })
  },
  cycleVariant(step: 1 | -1) {
    const current = VARIANTS.findIndex((v) => v.key === state.variant)
    actions.setVariant(VARIANTS[(current + step + VARIANTS.length) % VARIANTS.length].key)
  },
  setPickerOpen(open: boolean) {
    if (open === state.pickerOpen) return
    if (open && !state.noticeSeen) {
      set({ pickerOpen: true, noticeSeen: true, noticeSeenBy: "picker opened", lastAction: "picker opened; notice marked seen" })
      return
    }
    set({ pickerOpen: open, lastAction: open ? "picker opened" : "picker closed" })
  },
  pick(id: string) {
    if (id === state.activeProfileId) return
    set({ activeProfileId: id, lastAction: `this browser now uses ${nameOf(id)}` })
  },
  createFromActive(name: string) {
    const source = activeProfile()
    const id = `p${Date.now().toString(36)}`
    const profile: DisplayProfile = {
      ...structuredClone(source),
      id,
      name: name.trim(),
      isDefault: false,
      copiedFrom: source.name,
    }
    const profiles = [...state.profiles, profile]
    const noticePatch = !state.noticeSeen && profiles.length >= 2 ? { noticeSeen: true, noticeSeenBy: "a second profile exists" } : {}
    set({
      ...noticePatch,
      profiles,
      activeProfileId: id,
      lastAction: `created "${profile.name}" as a copy of ${source.name} (columns, both switches, sort and filters); this browser now uses it`,
    })
  },
  setOwnSortAndFilters(id: string, on: boolean) {
    set({
      profiles: updateProfile(id, (p) => (p.isDefault ? p : { ...p, ownSortAndFilters: on ? { ...state.sharedSortAndFilters } : null })),
      lastAction: on
        ? `${nameOf(id)}: Own sort and filters on, starts from a copy of the shared ones`
        : `${nameOf(id)}: Own sort and filters off; its own values are deleted`,
    })
  },
  /** On: the setup of instanceId goes to every instance. Off: each instance starts from a copy of the shared setup. */
  setSameColumns(id: string, on: boolean, instanceId: number | null) {
    const profile = profileById(state, id)
    if (sameColumns(profile) === on) return
    if (on) {
      const from = instanceId ?? state.lastInstanceId
      const source = columnsFor(profile, from).setup
      set({
        profiles: updateProfile(id, (p) => ({ ...p, sameColumnsSet: true, sharedColumns: structuredClone(source), perInstanceColumns: {} })),
        lastAction: `${profile.name}: Same columns on every instance on; the ${instanceLabel(from)} setup now shows on every instance`,
      })
      return
    }
    const copies = Object.fromEntries(PROTO_INSTANCES.map((i) => [i.id, structuredClone(profile.sharedColumns)]))
    set({
      profiles: updateProfile(id, (p) => ({ ...p, sameColumnsSet: false, perInstanceColumns: copies })),
      lastAction: `${profile.name}: Same columns on every instance off; each instance starts from a copy of the shared setup`,
    })
  },
  rename(id: string, name: string) {
    const before = nameOf(id)
    set({
      profiles: updateProfile(id, (p) => ({ ...p, name: name.trim() })),
      lastAction: `renamed ${before} → ${name.trim()}`,
    })
  },
  remove(id: string) {
    const target = profileById(state, id)
    if (target.isDefault) return
    const wasActive = state.activeProfileId === id
    set({
      profiles: state.profiles.filter((p) => p.id !== id),
      activeProfileId: wasActive ? "default" : state.activeProfileId,
      lastAction: `deleted ${target.name}${wasActive ? "; this browser moved to Default" : ""}`,
    })
  },
  /** Another browser deletes the profile this browser uses. */
  simulateRemoteDelete() {
    const target = activeProfile()
    if (target.isDefault) {
      toast("This browser uses Default, which cannot be deleted. Pick another profile first.")
      return
    }
    set({
      profiles: state.profiles.filter((p) => p.id !== target.id),
      activeProfileId: "default",
      lastAction: `another browser deleted ${target.name}; this browser moved to Default`,
    })
    toast(`The ${target.name} profile was deleted. This browser now uses ${nameOf("default")}.`)
  },
  dismissNotice() {
    set({ noticeSeen: true, noticeSeenBy: "Got it", lastAction: "notice: Got it" })
  },
  noticeAction() {
    set({ noticeSeen: true, noticeSeenBy: "Show profiles", pickerOpen: true, lastAction: "notice: Show profiles → picker opened" })
  },
  showNoticeAgain() {
    set({ noticeForced: true, noticeSeen: false, noticeSeenBy: "", lastAction: "notice reset to not seen" })
  },
  /** A fresh install: only Default exists and the notice was never seen. */
  firstRun() {
    const fresh = initialState()
    set({
      ...fresh,
      variant: state.variant,
      currentInstanceId: state.currentInstanceId,
      lastInstanceId: state.lastInstanceId,
      noticeForced: false,
      noticeSeen: false,
      noticeSeenBy: "",
      activeProfileId: "default",
      profiles: fresh.profiles.filter((p) => p.isDefault),
      lastAction: "first run: only Default exists",
    })
  },
  reset() {
    set({ ...initialState(), variant: state.variant, currentInstanceId: state.currentInstanceId, lastAction: "reset" })
  },
}

// A changed store module must not mix with state from the old one: reload the page instead of hot-swapping.
if (import.meta.hot) {
  import.meta.hot.accept(() => window.location.reload())
}
