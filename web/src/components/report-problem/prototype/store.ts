/*
 * PROTOTYPE, throwaway. One in-memory store for the prototype, so that the sidebar entry, the
 * Settings tab, and the switcher bar share the same state.
 */

import { useEffect, useState, useSyncExternalStore } from "react"
import type { Identity, Lane, Source, View } from "./mock"

export type Variant = "A" | "B" | "C"

export interface ProtoStore {
  variant: Variant | null
  open: boolean
  /** The new-feature bubble on the sidebar entry. */
  notice: boolean
  identity: Identity
  lane: Lane
  source: Source
  view: View
}

function readVariant(): Variant | null {
  const value = new URLSearchParams(window.location.search).get("variant") ?? sessionStorage.getItem("proto-report-variant")
  return value === "A" || value === "B" || value === "C" ? value : null
}

let state: ProtoStore = {
  variant: readVariant(),
  open: false,
  notice: true,
  identity: "anonymous",
  lane: "open",
  source: "menu",
  view: "compose",
}
const listeners = new Set<() => void>()

export function setProto(patch: Partial<ProtoStore>) {
  state = { ...state, ...patch }
  if (patch.variant) {
    const url = new URL(window.location.href)
    url.searchParams.set("variant", patch.variant)
    window.history.replaceState(window.history.state, "", url)
    sessionStorage.setItem("proto-report-variant", patch.variant)
  }
  listeners.forEach((fn) => fn())
}

export function useProto(): ProtoStore {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => state,
  )
}

/** True at lg and wider, where the sidebar shows. Below it the sidebar stays mounted but hidden. */
export function useDesktop() {
  const query = "(min-width: 1024px)"
  const [desktop, setDesktop] = useState(() => window.matchMedia(query).matches)
  useEffect(() => {
    const list = window.matchMedia(query)
    const sync = () => setDesktop(list.matches)
    list.addEventListener("change", sync)
    return () => list.removeEventListener("change", sync)
  }, [])
  return desktop
}

/** Keeps a modal open when the switcher bar is clicked. */
export function keepOpenForSwitcher(event: { target: EventTarget | null; preventDefault: () => void }) {
  if ((event.target as HTMLElement | null)?.closest("[data-proto-switcher]")) event.preventDefault()
}
