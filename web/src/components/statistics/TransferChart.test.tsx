/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { cloneElement, isValidElement, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TransferChart } from "@/components/statistics/TransferChart"
import "@/i18n"
import type { StatisticsBar } from "@/lib/statistics"

const i18n = vi.hoisted(() => ({
  t: (key: string, options?: Record<string, unknown>) => options?.groups === undefined ? key : `${key} groups=${String(options.groups)}`,
  i18n: { resolvedLanguage: "en", language: "en" },
}))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => i18n,
}))

// jsdom has no layout, so the chart gets a fixed size in place of the size of its container.
vi.mock("recharts", async (importOriginal) => ({
  ...await importOriginal<typeof import("recharts")>(),
  ResponsiveContainer: ({ children }: { children: ReactNode }) => isValidElement(children) ? cloneElement(children, { width: 800, height: 300 } as object) : null,
}))

afterEach(cleanup)

function bar(start: string, gapSeconds: number): StatisticsBar {
  return { start: Date.parse(start), width: "hour", gapSeconds, bytes: new Map([["a.test", { uploaded: 100, downloaded: 0 }]]) }
}

describe("TransferChart", () => {
  it("marks each bar that includes a Recording gap", () => {
    const bars = [bar("2026-01-10T10:00:00Z", 0), bar("2026-01-10T11:00:00Z", 5400), bar("2026-01-10T12:00:00Z", 0)]
    const { container } = render(<TransferChart bars={bars} groups={["a.test"]} metric="uploaded" unit="hour" displayName={group => group} />)

    expect(container.querySelectorAll(".recharts-reference-line").length).toBe(1)
  })

  it("names how many groups fold into Other", () => {
    const bytes = (uploaded: number) => ({ uploaded, downloaded: 0 })
    const bars: StatisticsBar[] = [
      { start: Date.parse("2026-01-10T10:00:00Z"), width: "hour", gapSeconds: 0, bytes: new Map([["a.test", bytes(100)], ["b.test", bytes(5)], ["c.test", bytes(0)]]) },
      { start: Date.parse("2026-01-10T11:00:00Z"), width: "hour", gapSeconds: 0, bytes: new Map([["a.test", bytes(100)], ["d.test", bytes(5)]]) },
    ]
    const { container } = render(<TransferChart bars={bars} groups={["a.test"]} metric="uploaded" unit="hour" displayName={group => group} />)

    expect(container.textContent).toContain("chart.otherCount groups=2")
  })
})
