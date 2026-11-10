/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: floating bar that cycles the variants of a UI prototype. It does not render in a production build.

import { ChevronLeft, ChevronRight } from "lucide-react"
import type { ReactNode } from "react"
import { useEffect } from "react"

interface PrototypeSwitcherProps {
  variants: { key: string; name: string }[]
  current: string
  onChange: (key: string) => void
  extra?: ReactNode
}

export function PrototypeSwitcher({ variants, current, onChange, extra }: PrototypeSwitcherProps) {
  const index = Math.max(0, variants.findIndex(v => v.key === current))
  const step = (delta: number) => onChange(variants[(index + delta + variants.length) % variants.length].key)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target?.closest("input, textarea, select, [contenteditable], [role='tablist'], [role='combobox'], [role='listbox']")) return
      if (e.key === "ArrowLeft") step(-1)
      if (e.key === "ArrowRight") step(1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  if (!import.meta.env.DEV) return null

  return (
    <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-full border border-fuchsia-400 bg-fuchsia-600 px-1.5 py-1 text-sm font-medium text-white shadow-xl">
      <button type="button" className="rounded-full p-1.5 hover:bg-white/20" onClick={() => step(-1)} aria-label="Previous variant">
        <ChevronLeft className="size-4" />
      </button>
      <span className="min-w-[11rem] whitespace-nowrap px-1 text-center">
        {variants[index].key} ({variants[index].name})
      </span>
      <button type="button" className="rounded-full p-1.5 hover:bg-white/20" onClick={() => step(1)} aria-label="Next variant">
        <ChevronRight className="size-4" />
      </button>
      {extra && <span className="ml-1 border-l border-white/30 pl-2 pr-1">{extra}</span>}
    </div>
  )
}
