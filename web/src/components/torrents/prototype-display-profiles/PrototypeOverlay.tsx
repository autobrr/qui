/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the floating variant switcher, the state debug panel,
// and the Manage profiles dialog. Rendered only in dev builds.

import { Bug, ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from "lucide-react"
import { useEffect, useState } from "react"

import { ManageProfilesDialog } from "./ManageProfilesDialog"
import { actions, PROTOTYPE_ENABLED, summarize, useActiveProfile, usePrototype, VARIANTS } from "./store"

function PrototypeSwitcher() {
  const variant = usePrototype((s) => s.variant)
  const meta = VARIANTS.find((v) => v.key === variant)!

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return
      const target = e.target as HTMLElement | null
      if (target?.closest("input, textarea, select, [contenteditable], [role=menu], [role=dialog], [role=listbox]")) return
      actions.cycleVariant(e.key === "ArrowLeft" ? -1 : 1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  return (
    <div
      data-prototype-switcher=""
      className="fixed bottom-12 left-1/2 z-[70] flex -translate-x-1/2 items-center gap-1 rounded-full bg-neutral-950 px-1.5 py-1 font-mono text-xs text-white shadow-2xl ring-2 ring-amber-400/80"
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

function DebugPanel() {
  const [open, setOpen] = useState(true)
  const s = usePrototype((state) => state)
  const active = useActiveProfile()

  return (
    <div
      data-prototype-debug=""
      className="fixed bottom-12 left-3 z-[70] w-[19rem] rounded-lg bg-neutral-950/95 font-mono text-[11px] leading-4 text-neutral-200 shadow-2xl ring-1 ring-amber-400/60"
    >
      <button type="button" onClick={() => setOpen(!open)} className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-amber-300">
        <Bug className="h-3.5 w-3.5" />
        prototype state
        {open ? <ChevronDown className="ml-auto h-3.5 w-3.5" /> : <ChevronUp className="ml-auto h-3.5 w-3.5" />}
      </button>
      {open && (
        <div className="flex flex-col gap-2 border-t border-white/10 px-2.5 pb-2.5 pt-2">
          <div>
            <div>this browser → <span className="text-amber-300">{active.name}</span> <span className="text-neutral-500">({s.activeProfileId}, localStorage)</span></div>
            <div className="text-neutral-400">
              picker {s.pickerOpen ? "open" : "closed"} · manage {s.manageOpen ? "open" : "closed"} · notice{" "}
              {!s.noticeForced ? "off" : s.noticeDismissed ? "dismissed" : "on"}
            </div>
          </div>
          <table className="w-full">
            <thead className="text-neutral-500">
              <tr><th className="text-left font-normal">profile (server)</th><th className="text-left font-normal">own sort</th></tr>
            </thead>
            <tbody>
              {s.profiles.map((p) => (
                <tr key={p.id} className={p.id === s.activeProfileId ? "text-amber-200" : undefined}>
                  <td className="py-0.5 pr-2 align-top">
                    {p.id === s.activeProfileId ? "▸ " : "  "}{p.name}{p.isDefault ? " (default)" : ""}
                    <div className="text-neutral-500">{summarize(p.setup)}</div>
                  </td>
                  <td className="py-0.5 align-top">{p.isDefault ? "—" : p.ownSortAndFilters ? "on" : "off"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="text-neutral-400">last: <span className="text-neutral-200">{s.lastAction}</span></div>
          <div className="flex flex-wrap gap-1">
            <button type="button" onClick={actions.simulateRemoteDelete} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">
              other browser deletes {active.name}
            </button>
            <button type="button" onClick={actions.showNoticeAgain} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">show notice</button>
            <button type="button" onClick={() => actions.setManageOpen(true)} className="rounded bg-white/10 px-1.5 py-0.5 hover:bg-white/20">open settings section</button>
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
  if (!PROTOTYPE_ENABLED) return null
  return (
    <>
      <ManageProfilesDialog />
      <DebugPanel />
      <PrototypeSwitcher />
    </>
  )
}
