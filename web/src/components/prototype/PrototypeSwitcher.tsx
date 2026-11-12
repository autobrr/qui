/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: floating bar that cycles the `?variant=` search param of the
// current route. Dev builds only.

import { useNavigate } from "@tanstack/react-router"
import { ChevronLeft, ChevronRight } from "lucide-react"
import { useEffect } from "react"

export interface PrototypeVariant {
  key: string
  name: string
}

interface PrototypeSwitcherProps {
  variants: PrototypeVariant[]
  current: string
  /** Extra state to show, for example the data source. A click cycles it. */
  toggle?: { label: string; onClick: () => void }
}

export function PrototypeSwitcher({ variants, current, toggle }: PrototypeSwitcherProps) {
  const navigate = useNavigate()
  const index = Math.max(0, variants.findIndex(v => v.key === current))

  const go = (step: number) => {
    const next = variants[(index + step + variants.length) % variants.length]
    void navigate({ to: ".", search: (prev: Record<string, unknown>) => ({ ...prev, variant: next.key }), replace: true })
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = document.activeElement
      if (el instanceof HTMLElement && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName))) return
      if (e.key === "ArrowLeft") go(-1)
      if (e.key === "ArrowRight") go(1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  if (!import.meta.env.DEV) return null

  return (
    <div className="fixed left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-full bg-fuchsia-600 p-1 text-sm font-medium text-white shadow-xl ring-2 ring-white/30 bottom-[calc(4.75rem+env(safe-area-inset-bottom))] lg:bottom-4">
      <button type="button" onClick={() => go(-1)} aria-label="Previous variant" className="grid size-9 place-items-center rounded-full hover:bg-white/20">
        <ChevronLeft className="size-4" />
      </button>
      <span className="min-w-[7.5rem] whitespace-nowrap px-1 text-center">{variants[index].name}</span>
      <button type="button" onClick={() => go(1)} aria-label="Next variant" className="grid size-9 place-items-center rounded-full hover:bg-white/20">
        <ChevronRight className="size-4" />
      </button>
      {toggle && (
        <button type="button" onClick={toggle.onClick} className="mr-1 whitespace-nowrap rounded-full bg-black/25 px-2.5 py-1.5 text-xs hover:bg-black/40">
          {toggle.label}
        </button>
      )}
    </div>
  )
}
