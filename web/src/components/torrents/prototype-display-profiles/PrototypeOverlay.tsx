/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the floating variant switcher (torrent page only) and
// the state debug panel (every page). Mounted once in AppLayout, dev builds only.

import { useLocation, useNavigate } from "@tanstack/react-router"
import { Bug, ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from "lucide-react"
import { useEffect, useState } from "react"

import { cn } from "@/lib/utils"
import {
  actions,
  columnsFor,
  PROTO_INSTANCES,
  PROTOTYPE_ENABLED,
  sameColumns,
  sameColumnsIsComputed,
  useActiveProfile,
  usePrototype,
  VARIANTS,
  visibleColumnCount
} from "./store"

function PrototypeSwitcher() {
  const variant = usePrototype((s) => s.variant)
  const meta = VARIANTS.find((v) => v.key === variant)!

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return
      const target = e.target as HTMLElement | null
      if (target?.closest("input, textarea, select, [contenteditable], [role=menu], [role=dialog], [role=listbox], [role=switch]")) return
      actions.cycleVariant(e.key === "ArrowLeft" ? -1 : 1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  return (
    <div
      data-prototype-switcher=""
      className="fixed bottom-12 left-1/2 z-[70] hidden -translate-x-1/2 items-center gap-1 rounded-full bg-neutral-950 px-1.5 py-1 font-mono text-xs text-white shadow-2xl ring-2 ring-amber-400/80 lg:flex"
    >
      <button type="button" onClick={() => actions.cycleVariant(-1)} aria-label="Previous variant" className="rounded-full p-1.5 hover:bg-white/15">
        <ChevronLeft className="h-4 w-4" />
      </button>
      <span className="px-1 text-center">
        <span className="font-bold text-amber-300">{variant}</span> ({meta.name})
      </span>
      <button type="button" onClick={() => actions.cycleVariant(1)} aria-label="Next variant" className="rounded-full p-1.5 hover:bg-white/15">
        <ChevronRight className="h-4 w-4" />
      </button>
    </div>
  )
}

function startsOpen() {
  try {
    const stored = window.localStorage.getItem("proto-display-profiles-debug")
    if (stored !== null) return stored === "open"
  } catch {
    // Private windows can refuse storage; fall back to the width.
  }
  return window.matchMedia("(min-width: 1024px)").matches
}

function DebugPanel() {
  const [open, setOpenState] = useState(startsOpen)
  const s = usePrototype((state) => state)
  const active = useActiveProfile()
  const navigate = useNavigate()

  const setOpen = (next: boolean) => {
    setOpenState(next)
    try {
      window.localStorage.setItem("proto-display-profiles-debug", next ? "open" : "closed")
    } catch {
      // Ignore: the panel still toggles.
    }
  }

  const noticeLabel = s.noticeSeen ? `seen (${s.noticeSeenBy})` : s.noticeForced ? "not seen (?notice=1)" : "not seen"

  return (
    <div
      data-prototype-debug=""
      className={cn(
        "fixed left-2 z-[70] rounded-lg bg-neutral-950/95 font-mono text-[11px] leading-4 text-neutral-200 shadow-2xl ring-1 ring-amber-400/60",
        "bottom-[calc(4.5rem+env(safe-area-inset-bottom))] lg:bottom-12",
        open ? "w-[min(21rem,calc(100vw-1rem))]" : "w-auto"
      )}
    >
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-amber-300"
      >
        <Bug className="h-3.5 w-3.5" />
        {open ? "prototype state" : "state"}
        {open ? <ChevronDown className="ml-auto h-3.5 w-3.5" /> : <ChevronUp className="ml-auto h-3.5 w-3.5" />}
      </button>
      {open && (
        <div className="flex max-h-[60dvh] flex-col gap-2 overflow-y-auto border-t border-white/10 px-2.5 pb-2.5 pt-2">
          <div>
            <div>this browser → <span className="text-amber-300">{active.name}</span> <span className="text-neutral-500">({s.activeProfileId}, localStorage)</span></div>
            <div className="text-neutral-400">
              picker {s.pickerOpen ? "open" : "closed"} · notice {noticeLabel}
            </div>
          </div>

          <div>
            <div className="text-neutral-500">
              {active.name} columns: {sameColumns(active) ? "same on every instance" : "per instance"}{" "}
              ({sameColumnsIsComputed(active) ? "computed" : "set"})
            </div>
            <table className="w-full">
              <tbody>
                {PROTO_INSTANCES.map((instance) => {
                  const { setup, source } = columnsFor(active, instance.id)
                  const here = s.currentInstanceId === instance.id
                  return (
                    <tr key={instance.id} className={here ? "text-amber-200" : undefined}>
                      <td className="py-0.5 pr-2">{here ? "▸ " : "  "}{instance.name}</td>
                      <td className="py-0.5 pr-2 text-neutral-400">reads {source}</td>
                      <td className="py-0.5 text-right">{visibleColumnCount(setup, instance.id)} cols{setup.stretchName ? " · stretch" : ""}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          <table className="w-full">
            <thead className="text-neutral-500">
              <tr>
                <th className="text-left font-normal">profile (server)</th>
                <th className="text-left font-normal">columns</th>
                <th className="text-left font-normal">sort, filters</th>
              </tr>
            </thead>
            <tbody>
              {s.profiles.map((p) => (
                <tr key={p.id} className={p.id === s.activeProfileId ? "text-amber-200" : undefined}>
                  <td className="py-0.5 pr-2 align-top">{p.id === s.activeProfileId ? "▸ " : "  "}{p.name}{p.isDefault ? " (default)" : ""}</td>
                  <td className="py-0.5 pr-2 align-top">
                    {sameColumns(p) ? "same" : `per inst. (${Object.keys(p.perInstanceColumns).length} stored)`}
                  </td>
                  <td className="py-0.5 align-top">
                    {p.ownSortAndFilters ? `own: ${p.ownSortAndFilters.sort}, ${p.ownSortAndFilters.filters}` : "shared"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="text-neutral-500">shared sort and filters: {s.sharedSortAndFilters.sort}, {s.sharedSortAndFilters.filters}</div>
          <div className="text-neutral-400">last: <span className="text-neutral-200">{s.lastAction}</span></div>
          <div className="flex flex-wrap gap-1">
            <button type="button" onClick={actions.simulateRemoteDelete} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">
              other browser deletes {active.name}
            </button>
            <button type="button" onClick={actions.showNoticeAgain} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">show notice</button>
            <button type="button" onClick={actions.firstRun} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">first run (Default only)</button>
            <button
              type="button"
              onClick={() => void navigate({ to: "/settings", search: { tab: "display-profiles" } })}
              className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20"
            >
              settings section
            </button>
            <button
              type="button"
              onClick={() => void navigate({ to: "/instances/$instanceId", params: { instanceId: "1" }, search: { variant: s.variant } as never })}
              className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20"
            >
              torrent page
            </button>
            <button type="button" onClick={actions.reset} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">reset</button>
          </div>
          <details>
            <summary className="cursor-pointer text-neutral-500">raw JSON</summary>
            <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-all text-[10px] text-neutral-400">
              {JSON.stringify({ activeProfileId: s.activeProfileId, profiles: s.profiles }, null, 1)}
            </pre>
          </details>
        </div>
      )}
    </div>
  )
}

export function DisplayProfilesPrototypeOverlay() {
  const pathname = useLocation({ select: (l) => l.pathname })
  if (!PROTOTYPE_ENABLED) return null
  return (
    <>
      <DebugPanel />
      {pathname.includes("/instances/") && <PrototypeSwitcher />}
    </>
  )
}
