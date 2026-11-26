/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
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

vi.mock("@/components/instances/preferences/SettingsSummary", () => ({
  SettingsSummary: ({ parts }: { parts: string[] }) => <span data-testid="settings-summary">{JSON.stringify(parts)}</span>,
}))

const { instancesQuery, settingsQuery, runsQuery, mutation } = vi.hoisted(() => {
  const instance = {
    id: 1,
    name: "seedbox-europe-west-archive-01",
    isActive: true,
    hasLocalFilesystemAccess: true,
    filesystemMode: "local",
    capabilities: { read: true, identity: true, trustedIdentity: true, write: true, content: true },
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
  useOrphanScanRunPaths: () => ({ data: undefined, isError: false }),
  useTriggerOrphanScan: () => mutation,
  useUpdateOrphanScanSettings: () => mutation,
  useCancelOrphanScanRun: () => mutation,
}))

import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"

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
  it("builds the settings summary from one key per pair", () => {
    const { getByTestId } = renderExpanded()

    expect(JSON.parse(getByTestId("settings-summary").textContent!)).toEqual([
      "preferences.orphanScanOverview.summaryGrace(10)",
      "preferences.orphanScanOverview.summaryInterval(24)",
      "preferences.orphanScanOverview.summaryMax(1000)",
    ])
  })
})
