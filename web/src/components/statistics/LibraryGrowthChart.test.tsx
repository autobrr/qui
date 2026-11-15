/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { cloneElement, isValidElement, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { LibraryGrowthChart } from "@/components/statistics/LibraryGrowthChart"
import "@/i18n"
import type { LibraryPoint } from "@/lib/statistics-library"

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
  ResponsiveContainer: ({ children }: { children: ReactNode }) => isValidElement(children) ? cloneElement(children, { width: 800, height: 240 } as object) : null,
}))

afterEach(cleanup)

const DAY = 86_400_000

function point(day: number, totalSize: number | null, uniqueSize: number | null): LibraryPoint {
  return { start: Date.parse("2026-01-10T00:00:00Z") + day * DAY, torrents: totalSize === null ? null : 1, totalSize, uniqueSize }
}

describe("LibraryGrowthChart", () => {
  it("draws a dot only for a day with a value between two gaps", () => {
    // The Unique size of day 3 sits between two gaps. The total size has a line through each day.
    const points = [point(0, 100, 50), point(1, 100, 50), point(2, 100, null), point(3, 100, 50), point(4, 100, null), point(5, 100, 50), point(6, 100, 50)]
    const { container } = render(<LibraryGrowthChart points={points} />)

    const dots = [...container.querySelectorAll(".recharts-area-dots circle")].filter(dot => dot.getAttribute("r") !== "0")
    expect(dots.length).toBe(1)
    expect(dots[0].getAttribute("fill")).toBe("var(--color-uniqueSize)")
  })
})
