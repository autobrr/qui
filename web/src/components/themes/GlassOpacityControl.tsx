/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Switch } from "@/components/ui/switch"
import { useGlassmorphism } from "@/hooks/useGlassmorphism"
import {
  GLASS_DEFAULT_PERCENT,
  GLASS_MIN_PERCENT,
  GLASS_SOLID_PERCENT,
  glassIsOn
} from "@/lib/glass"

function stopMenuEvent(event: { stopPropagation: () => void }) {
  event.stopPropagation()
}

/** Checked whenever the theme color is below 100%. The slider is that amount. */
export function GlassOpacityControl({ layout }: { layout: "menu" | "settings" }) {
  const { t } = useTranslation("common")
  const [percent, setPercent] = useGlassmorphism()
  const on = glassIsOn(percent)

  const toggle = () => {
    setPercent(on ? GLASS_SOLID_PERCENT : GLASS_DEFAULT_PERCENT)
  }

  const slider = (
    <input
      type="range"
      min={GLASS_MIN_PERCENT}
      max={GLASS_SOLID_PERCENT}
      step={5}
      value={percent}
      aria-label={t("themeToggle.glass")}
      aria-valuemin={GLASS_MIN_PERCENT}
      aria-valuemax={GLASS_SOLID_PERCENT}
      aria-valuenow={percent}
      onPointerDown={stopMenuEvent}
      onKeyDown={stopMenuEvent}
      onChange={(event) => setPercent(Number(event.target.value))}
      className="h-1.5 w-full cursor-pointer accent-foreground"
    />
  )

  if (layout === "settings") {
    return (
      <div className="rounded-md border px-3 py-2">
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm font-medium">{t("themeToggle.glass")}</span>
          <Switch
            checked={on}
            onCheckedChange={toggle}
            aria-label={t("themeToggle.glass")}
          />
        </div>
        <div className="mt-3">{slider}</div>
      </div>
    )
  }

  return (
    <div className="px-2 py-1.5" onPointerDown={stopMenuEvent}>
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none hover:bg-accent hover:text-accent-foreground"
        onClick={toggle}
      >
        <span className="flex-1 text-left">{t("themeToggle.glass")}</span>
        <span className="text-xs text-muted-foreground tabular-nums">{percent}%</span>
        {on ? <Check className="h-4 w-4" /> : <span className="h-4 w-4" />}
      </button>
      <div className="px-2 pb-1">{slider}</div>
    </div>
  )
}
