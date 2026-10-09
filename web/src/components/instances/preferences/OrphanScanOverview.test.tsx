/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Instance, OrphanScanRun, OrphanScanSettings } from "@/types"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) => opts ? `${key}(${Object.values(opts).join(",")})` : key,
    }),
  }
})

vi.mock("@/components/instances/preferences/OrphanScanPreviewDialog", () => ({
  OrphanScanPreviewDialog: () => null,
}))

// Stable singletons: a fresh object per render would rerun effects forever.
const { instancesQuery, settingsQuery, runsQuery, mutation } = vi.hoisted(() => {
  const instance = {
    id: 1,
    name: "seedbox-europe-west-archive-01",
    isActive: true,
    hasLocalFilesystemAccess: true,
    filesystemMode: "local",
    capabilities: { read: true, identity: true, write: true, content: true },
  }
  const settings = {
    instanceId: 1,
    enabled: true,
    gracePeriodMinutes: 10,
    ignorePaths: [],
    scanIntervalHours: 24,
    previewSort: "size_desc",
    maxFilesPerRun: 1000,
    autoCleanupEnabled: false,
    autoCleanupMaxFiles: 100,
    scanDefaultSavePath: true,
    scanCategoryPaths: true,
    deleteAbandonedDirs: false,
  }
  const run = {
    id: 1,
    instanceId: 1,
    status: "preview_ready",
    triggeredBy: "manual",
    scanPaths: [],
    filesFound: 3,
    filesDeleted: 0,
    foldersDeleted: 0,
    bytesReclaimed: 1024,
    truncated: false,
    startedAt: "2026-01-01T00:00:00Z",
  }
  return {
    instancesQuery: { instances: [instance as unknown as Instance] },
    settingsQuery: { data: settings as unknown as OrphanScanSettings },
    runsQuery: { data: [run as unknown as OrphanScanRun], isLoading: false },
    mutation: { isPending: false, mutate: vi.fn() },
  }
})

vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => instancesQuery,
}))

vi.mock("@/hooks/useOrphanScan", () => ({
  useOrphanScanSettings: () => settingsQuery,
  useOrphanScanRuns: () => runsQuery,
  useTriggerOrphanScan: () => mutation,
  useUpdateOrphanScanSettings: () => mutation,
  useCancelOrphanScanRun: () => mutation,
}))

import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"

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
})

function renderExpanded() {
  return render(
    <TooltipProvider>
      <OrphanScanOverview expandedInstances={["1"]} onExpandedInstancesChange={() => {}} />
    </TooltipProvider>
  )
}

describe("OrphanScanOverview instance card", () => {
  it("wraps a long instance name instead of cutting it off", () => {
    const { container } = renderExpanded()

    const name = Array.from(container.querySelectorAll("span")).find((el) => el.textContent === "seedbox-europe-west-archive-01")
    expect(name).toBeDefined()
    // Two names that share a long prefix look identical once truncated.
    expect(name!.classList.contains("truncate")).toBe(false)
    expect(name!.classList.contains("wrap-anywhere")).toBe(true)
  })

  it("wraps the View Preview label instead of cutting it off", () => {
    const { container } = renderExpanded()

    const label = Array.from(container.querySelectorAll("button span")).find((el) => el.textContent === "preferences.orphanScanOverview.viewPreview")
    expect(label).toBeDefined()
    expect(label!.classList.contains("truncate")).toBe(false)
    expect(label!.classList.contains("whitespace-normal")).toBe(true)
    // A fixed height would clip the second line of a wrapped label.
    expect(label!.closest("button")!.classList.contains("h-auto")).toBe(true)
  })

  it("builds the settings summary from one key per pair and keeps each pair on one line", () => {
    const { container } = renderExpanded()

    const spans = Array.from(container.querySelectorAll("p > span.whitespace-nowrap"))
    const pairs = spans.map((el) => el.textContent)
    expect(pairs).toEqual([
      "preferences.orphanScanOverview.summaryGrace(10)\u00a0·",
      "preferences.orphanScanOverview.summaryInterval(24)\u00a0·",
      "preferences.orphanScanOverview.summaryMax(1000)",
    ])
    // Only a plain space between pairs, so a wrapped line never starts with "·".
    expect(spans[0]!.parentElement!.textContent).toBe(pairs.join(" "))
  })
})
