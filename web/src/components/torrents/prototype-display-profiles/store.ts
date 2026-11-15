/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): three variants of the Display profile picker on the
// existing /instances/$id route, switchable via ?variant=A|B|C, ?notice=1 forces
// the Feature notice. All state lives in this module; nothing writes Client settings.

import { useSyncExternalStore } from "react"
import { toast } from "sonner"

export const PROTOTYPE_ENABLED = import.meta.env.DEV

export type VariantKey = "A" | "B" | "C"

export const VARIANTS: { key: VariantKey; name: string }[] = [
  { key: "A", name: "Toolbar button" },
  { key: "B", name: "Inside Toggle columns" },
  { key: "C", name: "Status bar chip" },
]

export type RowDensity = "Table" | "Dense" | "Stacked"

/** The Display settings a profile carries. Real ones would hold column ids and widths. */
export interface DisplaySetup {
  visibleColumns: number
  rowDensity: RowDensity
  stretchName: boolean
  filterSidebar: "open" | "closed"
  openFilterSections: string[]
  showEmptyFilters: boolean
}

export interface DisplayProfile {
  id: string
  name: string
  isDefault: boolean
  /** Off: sort and filters are shared with every other profile. Always off on Default. */
  ownSortAndFilters: boolean
  setup: DisplaySetup
  copiedFrom?: string
}

interface PrototypeState {
  variant: VariantKey
  noticeForced: boolean
  noticeDismissed: boolean
  pickerOpen: boolean
  manageOpen: boolean
  /** This browser's pick. Stored in localStorage in the real feature; absent means Default. */
  activeProfileId: string
  profiles: DisplayProfile[]
  lastAction: string
}

function initialProfiles(): DisplayProfile[] {
  return [
    {
      id: "default",
      name: "Default",
      isDefault: true,
      ownSortAndFilters: false,
      setup: { visibleColumns: 14, rowDensity: "Table", stretchName: true, filterSidebar: "open", openFilterSections: ["Status", "Categories", "Tags", "Trackers"], showEmptyFilters: true },
    },
    {
      id: "laptop",
      name: "Laptop",
      isDefault: false,
      ownSortAndFilters: false,
      setup: { visibleColumns: 7, rowDensity: "Dense", stretchName: true, filterSidebar: "closed", openFilterSections: ["Status"], showEmptyFilters: false },
    },
    {
      id: "phone",
      name: "Phone",
      isDefault: false,
      ownSortAndFilters: true,
      setup: { visibleColumns: 4, rowDensity: "Stacked", stretchName: false, filterSidebar: "closed", openFilterSections: [], showEmptyFilters: false },
    },
  ]
}

function readParams(): Pick<PrototypeState, "variant" | "noticeForced"> {
  const params = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search)
  const raw = params.get("variant")?.toUpperCase()
  const variant = VARIANTS.some((v) => v.key === raw) ? (raw as VariantKey) : "A"
  return { variant, noticeForced: params.get("notice") === "1" }
}

function initialState(): PrototypeState {
  return {
    ...readParams(),
    noticeDismissed: false,
    pickerOpen: false,
    manageOpen: false,
    activeProfileId: "laptop",
    profiles: initialProfiles(),
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

export function useActiveProfile(): DisplayProfile {
  const profiles = usePrototype((s) => s.profiles)
  const activeId = usePrototype((s) => s.activeProfileId)
  return profiles.find((p) => p.id === activeId) ?? profiles[0]
}

export function useNoticeVisible(): boolean {
  return usePrototype((s) => s.noticeForced && !s.noticeDismissed && !s.pickerOpen && !s.manageOpen)
}

function nameOf(id: string) {
  return state.profiles.find((p) => p.id === id)?.name ?? id
}

/** Returns an error message, or null when the name can be used. */
export function validateName(name: string, exceptId?: string): string | null {
  const trimmed = name.trim()
  if (!trimmed) return "Enter a name."
  if (state.profiles.some((p) => p.id !== exceptId && p.name.toLowerCase() === trimmed.toLowerCase())) {
    return "A profile with this name exists."
  }
  return null
}

export function summarize(setup: DisplaySetup): string {
  return `${setup.visibleColumns} columns · ${setup.rowDensity}${setup.stretchName ? " · Name stretch" : ""}`
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
    set({ pickerOpen: open, lastAction: open ? "picker opened" : "picker closed" })
  },
  pick(id: string) {
    if (id === state.activeProfileId) return
    set({ activeProfileId: id, lastAction: `this browser now uses ${nameOf(id)}` })
  },
  createFromActive(name: string) {
    const source = state.profiles.find((p) => p.id === state.activeProfileId)!
    const id = `p${Date.now().toString(36)}`
    const profile: DisplayProfile = {
      id,
      name: name.trim(),
      isDefault: false,
      ownSortAndFilters: false,
      setup: structuredClone(source.setup),
      copiedFrom: source.name,
    }
    set({
      profiles: [...state.profiles, profile],
      activeProfileId: id,
      lastAction: `created "${profile.name}" as a copy of ${source.name}; this browser now uses it`,
    })
  },
  setOwnSortAndFilters(id: string, on: boolean) {
    set({
      profiles: state.profiles.map((p) => (p.id === id && !p.isDefault ? { ...p, ownSortAndFilters: on } : p)),
      lastAction: `${nameOf(id)}: Own sort and filters ${on ? "on" : "off"}`,
    })
  },
  rename(id: string, name: string) {
    const before = nameOf(id)
    set({
      profiles: state.profiles.map((p) => (p.id === id ? { ...p, name: name.trim() } : p)),
      lastAction: `renamed ${before} → ${name.trim()}`,
    })
  },
  remove(id: string) {
    const target = state.profiles.find((p) => p.id === id)
    if (!target || target.isDefault) return
    const wasActive = state.activeProfileId === id
    set({
      profiles: state.profiles.filter((p) => p.id !== id),
      activeProfileId: wasActive ? "default" : state.activeProfileId,
      lastAction: `deleted ${target.name}${wasActive ? "; this browser moved to Default" : ""}`,
    })
  },
  /** Another browser deletes the profile this browser uses. */
  simulateRemoteDelete() {
    const target = state.profiles.find((p) => p.id === state.activeProfileId)
    if (!target || target.isDefault) {
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
  setManageOpen(open: boolean) {
    set({ manageOpen: open, pickerOpen: false, lastAction: open ? "Manage profiles opened" : "Manage profiles closed" })
  },
  dismissNotice() {
    set({ noticeDismissed: true, lastAction: "notice: Got it" })
  },
  noticeAction() {
    set({ noticeDismissed: true, pickerOpen: true, lastAction: "notice: Show profiles → picker opened" })
  },
  showNoticeAgain() {
    set({ noticeForced: true, noticeDismissed: false, lastAction: "notice shown again" })
  },
  reset() {
    set({ ...initialState(), variant: state.variant, noticeForced: state.noticeForced, lastAction: "reset" })
  },
}
