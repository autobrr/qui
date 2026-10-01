/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Instance } from "@/types"

// Keep the module's other exports (initReactI18next is pulled in through the import graph).
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatISOTimestamp: (iso: string) => iso }),
}))

// Stable singletons stand in for React Query's cache: reopening the preview gets
// the same first-page object back, which is what made a reused dialog show nothing.
const { instances, runsQuery, firstPage, secondPage, setFilesFound, idleQuery, idleMutation } = vi.hoisted(() => {
  const run = {
    id: 1, instanceId: 1, status: "preview_ready", triggeredBy: "manual", scanPaths: [],
    filesFound: 3, filesDeleted: 0, foldersDeleted: 0, bytesReclaimed: 60, truncated: false,
    startedAt: "2026-01-01T00:00:00Z",
  }
  const files = [
    { id: 1, runId: 1, filePath: "/data/a.mkv", fileSize: 10, isAbandonedDir: false, status: "pending" },
    { id: 2, runId: 1, filePath: "/data/b.mkv", fileSize: 20, isAbandonedDir: false, status: "pending" },
    { id: 3, runId: 1, filePath: "/data/leftover", fileSize: 0, isAbandonedDir: true, status: "pending" },
  ]
  const firstPage = { data: { ...run, files, partial: false, errorMessage: "" } }
  const secondPage = {
    data: {
      ...run,
      files: [{ id: 4, runId: 1, filePath: "/data/d.mkv", fileSize: 40, isAbandonedDir: false, status: "pending" }],
      partial: false,
      errorMessage: "",
    },
  }
  return {
    instances: [{ id: 1, name: "Seedbox", isActive: true, hasLocalFilesystemAccess: true }],
    runsQuery: { data: [run], isLoading: false },
    firstPage,
    secondPage,
    setFilesFound: (count: number) => {
      for (const r of [run, firstPage.data, secondPage.data]) r.filesFound = count
    },
    idleQuery: { data: undefined, isLoading: false },
    idleMutation: { isPending: false, mutate: () => {} },
  }
})

vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => ({ instances: instances as unknown as Instance[] }),
}))

vi.mock("@/hooks/useOrphanScan", () => ({
  useOrphanScanSettings: () => idleQuery,
  useOrphanScanRuns: () => runsQuery,
  useOrphanScanRun: (_instanceId: number, _runId: number, options?: { offset?: number }) =>
    options?.offset ? secondPage : firstPage,
  useTriggerOrphanScan: () => idleMutation,
  useUpdateOrphanScanSettings: () => idleMutation,
  useCancelOrphanScanRun: () => idleMutation,
  useConfirmOrphanScanDeletion: () => idleMutation,
}))

import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"

beforeEach(() => {
  setFilesFound(3)
  // Radix dialog/tooltip measure through ResizeObserver, which jsdom lacks.
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
      <OrphanScanOverview expandedInstances={["1"]} onExpandedInstancesChange={() => {}} />
    </TooltipProvider>
  )
}

const rowPaths = () => [...document.body.querySelectorAll("tbody td:first-child")].map((td) => td.textContent)
const firstPagePaths = firstPage.data.files.map((f) => f.filePath)

function button(name: string) {
  const match = [...document.body.querySelectorAll("button")].find((b) => b.textContent === name)
  if (!match) throw new Error(`no button ${name}`)
  return match
}

const openPreview = () => fireEvent.click(button("preferences.orphanScanOverview.viewPreview"))
const closePreview = () => fireEvent.click(button("actions.close"))

it("shows the files again each time the preview is reopened", () => {
  renderOverview()

  openPreview()
  expect(rowPaths()).toEqual(firstPagePaths)

  for (let i = 0; i < 2; i++) {
    closePreview()
    expect(rowPaths()).toEqual([])
    openPreview()
    expect(rowPaths()).toEqual(firstPagePaths)
  }
})

it("starts from the first page after loading more and reopening", () => {
  setFilesFound(4)
  renderOverview()

  openPreview()
  fireEvent.click(button("preferences.orphanScanPreview.loadMore"))
  expect(rowPaths()).toEqual([...firstPagePaths, "/data/d.mkv"])

  closePreview()
  openPreview()
  expect(rowPaths()).toEqual(firstPagePaths)

  fireEvent.click(button("preferences.orphanScanPreview.loadMore"))
  expect(rowPaths()).toEqual([...firstPagePaths, "/data/d.mkv"])
})
