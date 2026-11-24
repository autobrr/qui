/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, within } from "@testing-library/react"
import { cloneElement, isValidElement, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TransferChart } from "@/components/statistics/TransferChart"
import "@/i18n"
import { _resetClientSettingsForTests, writeRaw } from "@/lib/client-settings"
import type { StatisticsBar } from "@/lib/statistics"

const i18n = vi.hoisted(() => ({
  t: (key: string, options?: Record<string, unknown>) => options?.groups === undefined ? key : `${key} groups=${String(options.groups)}`,
  i18n: { resolvedLanguage: "en", language: "en" },
}))
const trackers = vi.hoisted(() => ({
  icons: { data: { "a.test": "data:image/png;base64,YQ==" } } as { data?: Record<string, string> },
  customizations: { data: [] },
}))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => trackers.icons }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => trackers.customizations }))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => i18n,
}))

// jsdom has no layout, so the chart gets a fixed size in place of the size of its container.
vi.mock("recharts", async (importOriginal) => ({
  ...await importOriginal<typeof import("recharts")>(),
  ResponsiveContainer: ({ children }: { children: ReactNode }) => isValidElement(children) ? cloneElement(children, { width: 800, height: 300 } as object) : null,
}))

const measuredTextWidth = { value: 40 }
Object.defineProperty(SVGElement.prototype, "getComputedTextLength", { configurable: true, value: () => measuredTextWidth.value })

afterEach(() => {
  measuredTextWidth.value = 40
  cleanup()
  localStorage.clear()
  _resetClientSettingsForTests()
})

function bar(start: string, gapSeconds: number): StatisticsBar {
  return { start: Date.parse(start), width: "hour", gapSeconds, bytes: new Map([["a.test", { uploaded: 100, downloaded: 0 }]]) }
}

const twoDaysOfHours = Array.from({ length: 24 }, (_, index) => ({ ...bar("2026-01-10T00:00:00Z", 0), start: new Date(2026, 0, 10, 12 + index).getTime() }))

function dayNames(container: HTMLElement): SVGTextElement[] {
  return [...container.querySelectorAll<SVGTextElement>("text")].filter(text => text.textContent?.includes(", Jan "))
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

  it("shows the tracker icon of each Tracker group in the legend, and none for Other", () => {
    const bytes = (uploaded: number) => ({ uploaded, downloaded: 0 })
    const bars: StatisticsBar[] = [
      { start: Date.parse("2026-01-10T10:00:00Z"), width: "hour", gapSeconds: 0, bytes: new Map([["a.test", bytes(100)], ["b.test", bytes(5)], ["c.test", bytes(5)]]) },
    ]
    const { container } = render(<TransferChart bars={bars} groups={["a.test", "b.test"]} metric="uploaded" unit="hour" displayName={group => group} />)

    const legend = (name: string) => [...container.querySelectorAll(".recharts-legend-wrapper span.inline-flex")].find(item => item.textContent?.endsWith(name))
    expect(legend("a.test")?.querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,YQ==")
    expect(legend("b.test")?.querySelector("span[aria-hidden=true]")?.textContent).toBe("B")
    expect(legend("chart.otherCount groups=1")?.querySelector("img, span[aria-hidden=true]")).toBeNull()
  })

  it("shows the tracker icon of each Tracker group in the tooltip of a bar, and none for Other", async () => {
    const bytes = (uploaded: number) => ({ uploaded, downloaded: 0 })
    const bars: StatisticsBar[] = [
      { start: Date.parse("2026-01-10T10:00:00Z"), width: "hour", gapSeconds: 0, bytes: new Map([["a.test", bytes(100)], ["b.test", bytes(5)], ["c.test", bytes(5)]]) },
    ]
    const { container } = render(<TransferChart bars={bars} groups={["a.test", "b.test"]} metric="uploaded" unit="hour" displayName={group => group} />)

    const wrapper = container.querySelector(".recharts-wrapper")!
    fireEvent.mouseEnter(wrapper, { clientX: 400, clientY: 150 })
    fireEvent.mouseMove(wrapper, { clientX: 400, clientY: 150 })
    const tooltip = await vi.waitFor(() => {
      const box = container.querySelector<HTMLElement>(".recharts-tooltip-wrapper .min-w-48")
      if (!box) throw new Error("no tooltip")
      return box
    })
    const row = (name: string) => within(tooltip).getByText(name).parentElement!
    expect(row("a.test").querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,YQ==")
    expect(row("b.test").querySelector("span[aria-hidden=true]")?.textContent).toBe("B")
    expect(row("chart.other").querySelector("img, span[aria-hidden=true]")).toBeNull()
  })

  it("names each day of an hour chart, and hides a name that is wider than its day", () => {
    const names = (container: HTMLElement) => dayNames(container).map(text => [text.textContent, text.style.visibility])
    const chart = <TransferChart bars={twoDaysOfHours} groups={["a.test"]} metric="uploaded" unit="hour" displayName={group => group} />

    expect(names(render(chart).container)).toEqual([["Sat, Jan 10", ""], ["Sun, Jan 11", ""]])
    cleanup()

    measuredTextWidth.value = 10_000
    expect(names(render(chart).container)).toEqual([["Sat, Jan 10", "hidden"], ["Sun, Jan 11", "hidden"]])
  })

  // regression: #4441
  it("draws the day names over the bars", () => {
    const { container } = render(<TransferChart bars={twoDaysOfHours} groups={["a.test"]} metric="uploaded" unit="hour" displayName={group => group} />)

    const lastBar = [...container.querySelectorAll(".recharts-bar-rectangle")].at(-1)!
    expect(dayNames(container).map(name => lastBar.compareDocumentPosition(name) & Node.DOCUMENT_POSITION_FOLLOWING))
      .toEqual([Node.DOCUMENT_POSITION_FOLLOWING, Node.DOCUMENT_POSITION_FOLLOWING])
  })

  it("colors the series from the selected set", () => {
    const bars = [bar("2026-01-10T10:00:00Z", 0)]
    const chart = <TransferChart bars={bars} groups={["a.test"]} metric="uploaded" unit="hour" displayName={group => group} />
    const style = (container: HTMLElement) => container.querySelector("style")?.textContent ?? ""

    const distinct = render(chart)
    expect(style(distinct.container)).toContain("--color-s0: #2a78d6;")
    expect(style(distinct.container)).toContain("--color-s0: #3987e5;")
    cleanup()

    writeRaw("qui-statistics-series-colors", JSON.stringify("theme"))
    const theme = render(chart)
    expect(style(theme.container)).toContain("--color-s0: var(--chart-1);")
    expect(style(theme.container)).not.toContain("#2a78d6")
  })
})
