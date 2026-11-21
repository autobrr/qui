/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"
import { ChevronLeft, ChevronRight, Eye, EyeOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useIncognitoMode } from "@/lib/incognito"

export const INCOGNITO_VARIANTS = {
  A: "Statistics header only",
  B: "Top bar, Dashboard button removed",
  C: "Top bar, Dashboard button kept",
} as const
export type IncognitoVariant = keyof typeof INCOGNITO_VARIANTS
const KEYS = Object.keys(INCOGNITO_VARIANTS) as IncognitoVariant[]
const STORAGE_KEY = "prototype-4329-variant"

const listeners = new Set<() => void>()
const fromUrl = new URLSearchParams(window.location.search).get("variant")?.toUpperCase()
if (fromUrl && KEYS.includes(fromUrl as IncognitoVariant)) localStorage.setItem(STORAGE_KEY, fromUrl)

function read(): IncognitoVariant {
  const value = localStorage.getItem(STORAGE_KEY) as IncognitoVariant | null
  return value && KEYS.includes(value) ? value : "A"
}

function write(value: IncognitoVariant) {
  localStorage.setItem(STORAGE_KEY, value)
  listeners.forEach(listener => listener())
}

export function useIncognitoVariant(): IncognitoVariant {
  return useSyncExternalStore(listener => {
    listeners.add(listener)
    return () => listeners.delete(listener)
  }, read)
}

export function IncognitoEyeButton({ className, variant = "outline" }: { className?: string; variant?: "outline" | "ghost" }) {
  const { t } = useTranslation("dashboard")
  const [incognito, setIncognito] = useIncognitoMode()
  return (
    <Button
      variant={variant}
      size="icon"
      className={className}
      aria-label={incognito ? t("incognito.turnOff") : t("incognito.turnOn")}
      aria-pressed={incognito}
      onClick={() => setIncognito(!incognito)}
    >
      {incognito ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
    </Button>
  )
}

export function IncognitoPrototypeSwitcher() {
  const variant = useIncognitoVariant()
  const [incognito] = useIncognitoMode()
  const step = (delta: number) => write(KEYS[(KEYS.indexOf(variant) + delta + KEYS.length) % KEYS.length])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement
      if (target.closest("input, textarea, [contenteditable]")) return
      if (event.key === "ArrowLeft") step(-1)
      if (event.key === "ArrowRight") step(1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  return (
    <div className="fixed top-2 lg:top-auto lg:bottom-4 left-1/2 z-[100] -translate-x-1/2 flex items-center gap-2 rounded-full bg-fuchsia-600 px-2 py-1 text-sm font-medium text-white shadow-lg">
      <button type="button" aria-label="Previous variant" className="rounded-full p-1 hover:bg-white/20" onClick={() => step(-1)}>
        <ChevronLeft className="h-4 w-4" />
      </button>
      <span className="whitespace-nowrap">#4329 · {variant}: {INCOGNITO_VARIANTS[variant]} · incognito {incognito ? "on" : "off"}</span>
      <button type="button" aria-label="Next variant" className="rounded-full p-1 hover:bg-white/20" onClick={() => step(1)}>
        <ChevronRight className="h-4 w-4" />
      </button>
    </div>
  )
}
