/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (discussion #4332): throwaway variant switcher. Do not merge.

import { ChevronLeft, ChevronRight } from "lucide-react"
import { useEffect } from "react"

export function PrototypeSwitcher({ variants, current, onChange }: { variants: { key: string; label: string }[]; current: string; onChange: (key: string) => void }) {
  const index = Math.max(0, variants.findIndex(variant => variant.key === current))
  const step = (by: number) => onChange(variants[(index + by + variants.length) % variants.length].key)

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target?.closest("input, textarea, [contenteditable]")) return
      if (event.key === "ArrowLeft") step(-1)
      if (event.key === "ArrowRight") step(1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  if (!import.meta.env.DEV) return null
  return (
    <div className="fixed bottom-4 left-1/2 z-[100] flex -translate-x-1/2 items-center gap-1 rounded-full border-2 border-fuchsia-500 bg-black px-2 py-1 text-sm text-white shadow-2xl">
      <button type="button" className="rounded-full p-1 hover:bg-white/20" onClick={() => step(-1)} aria-label="Previous variant"><ChevronLeft className="size-4" /></button>
      <span className="min-w-56 px-2 text-center font-mono text-xs">{variants[index].key} · {variants[index].label}</span>
      <button type="button" className="rounded-full p-1 hover:bg-white/20" onClick={() => step(1)} aria-label="Next variant"><ChevronRight className="size-4" /></button>
    </div>
  )
}
