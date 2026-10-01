/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Instance, InstanceReannounceSettings } from "@/types"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) => opts ? `${key}(${Object.values(opts).join(",")})` : key,
    }),
  }
})

vi.mock("@/components/instances/preferences/ReannounceEnableWarning", () => ({
  ReannounceEnableWarningDialog: () => null,
}))

// Stable singletons: a fresh object per render would rerun effects forever.
const { instancesQuery, activityQueries, queryClient, formatters } = vi.hoisted(() => {
  const settings = {
    enabled: true,
    initialWaitSeconds: 15,
    reannounceIntervalSeconds: 7,
    maxAgeSeconds: 600,
    maxRetries: 50,
    aggressive: true,
    monitorAll: true,
    excludeCategories: false,
    categories: [],
    excludeTags: false,
    tags: [],
    excludeTrackers: false,
    trackers: [],
  }
  const instance = {
    id: 1,
    name: "seedbox-europe-west-archive-01",
    host: "http://127.0.0.1:8080",
    isActive: true,
    reannounceSettings: settings,
  }
  return {
    instancesQuery: { instances: [instance as unknown as Instance], updateInstance: vi.fn(), isUpdating: false },
    activityQueries: [{ data: [], isFetching: false, isLoading: false, isError: false }],
    queryClient: { invalidateQueries: vi.fn() },
    formatters: { formatISOTimestamp: (value: string) => value },
  }
})

vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => instancesQuery,
}))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useActivityStream: () => {},
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => formatters,
}))

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>()
  return {
    ...actual,
    useQueries: () => activityQueries,
    useQueryClient: () => queryClient,
  }
})

import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"

const baseSettings = instancesQuery.instances[0].reannounceSettings

beforeEach(() => {
  // Radix switch and tooltip measure through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  instancesQuery.instances[0].reannounceSettings = baseSettings
})

function renderExpanded() {
  return render(
    <TooltipProvider>
      <ReannounceOverview expandedInstances={["1"]} onExpandedInstancesChange={() => {}} />
    </TooltipProvider>
  )
}

describe("ReannounceOverview instance card", () => {
  it("wraps a long instance name instead of cutting it off", () => {
    const { container } = renderExpanded()

    const name = Array.from(container.querySelectorAll("span")).find((el) => el.textContent === "seedbox-europe-west-archive-01")
    expect(name).toBeDefined()
    // Two names that share a long prefix look identical once truncated.
    expect(name!.classList.contains("truncate")).toBe(false)
    expect(name!.classList.contains("wrap-anywhere")).toBe(true)
  })

  it("keeps each settings summary pair on one line", () => {
    const { container } = renderExpanded()

    const spans = Array.from(container.querySelectorAll("p > span.whitespace-nowrap"))
    const pairs = spans.map((el) => el.textContent)
    expect(pairs).toEqual([
      "preferences.reannounceOverview.summaryWait(15) ·",
      "preferences.reannounceOverview.summaryRetry(7) ·",
      "preferences.reannounceOverview.summaryMax(50) ·",
      "preferences.reannounceOverview.summaryQuick",
    ])
    // Only a plain space between pairs, so a wrapped line never starts with "·".
    expect(spans[0]!.parentElement!.textContent).toBe(pairs.join(" "))
  })

  it("shows the not configured text when the instance has no reannounce settings", () => {
    instancesQuery.instances[0].reannounceSettings = undefined as unknown as InstanceReannounceSettings
    const { container } = renderExpanded()

    const summary = Array.from(container.querySelectorAll("p")).find((el) => el.textContent === "preferences.reannounceOverview.notConfigured")
    expect(summary).toBeDefined()
    expect(summary!.querySelector("span")).toBeNull()
  })
})
