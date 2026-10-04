/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { FilesystemMode, Instance } from "@/types"

// initReactI18next is pulled in through the import graph, so keep the module's other exports.
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

// The mocks hand back the same objects every render, since a fresh one would loop the components' effects.
const { instances, settingsQuery, runsQuery, mutation, useOrphanScanSettings } = vi.hoisted(() => {
  const instance = (id: number, filesystemMode: FilesystemMode, read = filesystemMode !== "none") => ({
    id,
    name: `instance-${id}-${filesystemMode}`,
    isActive: true,
    hasLocalFilesystemAccess: filesystemMode === "local",
    filesystemMode,
    capabilities: { read, identity: filesystemMode === "local", write: filesystemMode === "local", content: filesystemMode === "local" },
  })
  const settingsQuery: { data?: { autoCleanupEnabled: boolean, autoCleanupMaxFiles: number } } = { data: undefined }
  return {
    settingsQuery,
    // Instance 4 pairs a remote mode with no Read, so the gate is shown to follow the capability rather than the mode.
    instances: [instance(1, "local"), instance(2, "remote"), instance(3, "none"), instance(4, "remote", false)] as unknown as Instance[],
    runsQuery: { data: [], isLoading: false },
    mutation: { mutate: () => {}, isPending: false },
    useOrphanScanSettings: vi.fn(() => settingsQuery),
  }
})

vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => ({ instances }),
}))

vi.mock("@/hooks/useOrphanScan", () => ({
  useOrphanScanSettings,
  useOrphanScanRuns: () => runsQuery,
  useTriggerOrphanScan: () => mutation,
  useUpdateOrphanScanSettings: () => mutation,
  useCancelOrphanScanRun: () => mutation,
  useOrphanScanRun: () => ({ data: undefined }),
  useConfirmOrphanScanDeletion: () => mutation,
}))

import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"

beforeEach(() => {
  settingsQuery.data = undefined
  useOrphanScanSettings.mockClear()
  // Radix accordion and tooltip measure through ResizeObserver, which jsdom lacks.
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

function renderOverview() {
  return render(
    <TooltipProvider>
      <OrphanScanOverview expandedInstances={["1", "2", "3", "4"]} onExpandedInstancesChange={() => {}} />
    </TooltipProvider>
  )
}

it("offers orphan scan to a remote instance and blocks each one without Read", () => {
  const { container } = renderOverview()

  const text = container.textContent ?? ""
  expect(text.split("preferences.orphanScanOverview.noLocalAccess").length - 1).toBe(2)
  expect(text).toContain("instance-2-remote")
  expect(useOrphanScanSettings).toHaveBeenCalledWith(2, { enabled: true })
  expect(useOrphanScanSettings).toHaveBeenCalledWith(3, { enabled: false })
  expect(useOrphanScanSettings).toHaveBeenCalledWith(4, { enabled: false })
})

it("tells a remote instance inline what does not run on it", () => {
  const { container } = renderOverview()

  const text = container.textContent ?? ""
  expect(text.split("preferences.orphanScanOverview.remoteLimits").length - 1).toBe(1)
})

it("says auto-cleanup does not run for a remote instance even when it is enabled", () => {
  settingsQuery.data = { autoCleanupEnabled: true, autoCleanupMaxFiles: 100 }
  const { container } = renderOverview()

  // Only the local and remote instances render a summary. The one without access renders none.
  const text = container.textContent ?? ""
  expect(text.split("preferences.orphanScanOverview.autoCleanupRemote").length - 1).toBe(1)
  expect(text.split("preferences.orphanScanOverview.autoCleanupEnabled").length - 1).toBe(1)
})
