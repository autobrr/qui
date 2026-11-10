/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: five variants of the Statistics page (issue #3991), switchable via ?variant= on the
// throwaway route /prototype/statistics. All data is mock data. ?palette= swaps the series colors.
// Verdict: variant E is the layout of the first version. The series colors are a user choice
// between the distinct set and the theme chart colors.

import { PrototypeSwitcher } from "@/components/prototype/PrototypeSwitcher"
import { useState } from "react"
import type { Palette } from "./charts"
import { paletteClass } from "./charts"
import type { PeriodKey } from "./mock-data"
import { getStats } from "./mock-data"
import { VariantA } from "./VariantA"
import { VariantB } from "./VariantB"
import { VariantC } from "./VariantC"
import { VariantD } from "./VariantD"
import { VariantE } from "./VariantE"

export const VARIANTS = [
  { key: "A", name: "Overview", component: VariantA },
  { key: "B", name: "Ledger", component: VariantB },
  { key: "C", name: "Sections", component: VariantC },
  { key: "D", name: "Year in review", component: VariantD },
  { key: "E", name: "Combined", component: VariantE },
]

interface PrototypeStatisticsProps {
  variant: string
  palette: Palette
  onChange: (next: { variant?: string; palette?: Palette }) => void
}

export function PrototypeStatistics({ variant, palette, onChange }: PrototypeStatisticsProps) {
  const [period, setPeriod] = useState<PeriodKey>("30d")
  const [instance, setInstance] = useState("all")
  const current = VARIANTS.find(v => v.key === variant) ?? VARIANTS[0]
  const stats = getStats(current.key === "D" ? "1y" : period, instance)
  const Variant = current.component

  return (
    <div className={paletteClass(palette)}>
      <Variant stats={stats} period={period} setPeriod={setPeriod} instance={instance} setInstance={setInstance} />
      <PrototypeSwitcher
        variants={VARIANTS}
        current={current.key}
        onChange={key => onChange({ variant: key })}
        extra={
          <button type="button" className="whitespace-nowrap rounded-full px-2 py-1 hover:bg-white/20" onClick={() => onChange({ palette: palette === "theme" ? "categorical" : "theme" })}>
            Colors: {palette === "theme" ? "theme" : "distinct"}
          </button>
        }
      />
    </div>
  )
}
