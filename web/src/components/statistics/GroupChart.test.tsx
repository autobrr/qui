/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { cloneElement, isValidElement, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { GroupChart } from "@/components/statistics/GroupChart"
import "@/i18n"
import type { StatisticsBar } from "@/lib/statistics"

const i18n = vi.hoisted(() => ({
  t: (key: string) => key,
  i18n: { resolvedLanguage: "en", language: "en" },
}))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => i18n,
}))

// jsdom has no layout, so the chart gets a fixed size in place of the size of its container.
vi.mock("recharts", async (importOriginal) => ({
  ...await importOriginal<typeof import("recharts")>(),
  ResponsiveContainer: ({ children }: { children: ReactNode }) => isValidElement(children) ? cloneElement(children, { width: 800, height: 200 } as object) : null,
}))

afterEach(cleanup)

function bar(start: Date, width: StatisticsBar["width"]): StatisticsBar {
  return { start: start.getTime(), width, gapSeconds: 0, bytes: new Map([["h1", { uploaded: 100, downloaded: 0 }]]) }
}

describe("GroupChart", () => {
  // regression: #4113
  it("labels the bar of a folded year with its year", () => {
    const bars = [bar(new Date(2024, 0, 1), "year"), bar(new Date(2025, 0, 1), "year"), bar(new Date(2026, 0, 5), "day")]
    const { container } = render(<GroupChart bars={bars} unit="day" color={undefined} />)

    const ticks = [...container.querySelectorAll(".recharts-xAxis-tick-labels .recharts-cartesian-axis-tick-value")].map(tick => tick.textContent)
    expect(ticks).toEqual(["2024", "2025", new Date(2026, 0, 5).toLocaleDateString("en", { day: "numeric", month: "short" })])
  })
})
