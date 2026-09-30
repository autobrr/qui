/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"

// initReactI18next is pulled in through the import graph, so keep the module's other exports.
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

// The form's reset effect depends on the query data, so the mocks hand back the same objects every render.
const { settingsQuery, mutation } = vi.hoisted(() => ({
  settingsQuery: {
    isLoading: false,
    isError: false,
    data: {
      enabled: true,
      gracePeriodMinutes: 10,
      scanIntervalHours: 24,
      previewSort: "size_desc",
      maxFilesPerRun: 1000,
      ignorePaths: [],
      autoCleanupEnabled: false,
      autoCleanupMaxFiles: 100,
    },
  },
  mutation: { mutate: () => {}, isPending: false },
}))

vi.mock("@/hooks/useOrphanScan", () => ({
  useOrphanScanSettings: () => settingsQuery,
  useUpdateOrphanScanSettings: () => mutation,
}))

import { OrphanScanSettingsForm } from "@/components/instances/preferences/OrphanScanSettingsForm"

beforeEach(() => {
  settingsQuery.data.autoCleanupEnabled = false
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

function renderForm(isRemote: boolean) {
  return render(
    <TooltipProvider>
      <OrphanScanSettingsForm instanceId={1} isRemote={isRemote} />
    </TooltipProvider>
  )
}

it("notes beside auto-cleanup that it does not run over SSH", () => {
  const { container } = renderForm(true)

  expect(container.textContent).toContain("preferences.orphanScanOverview.remoteLimits")
  expect(container.querySelector("#auto-cleanup-enabled")).not.toBeNull()
})

it("shows no SSH note for a local instance", () => {
  const { container } = renderForm(false)

  expect(container.textContent).not.toContain("preferences.orphanScanOverview.remoteLimits")
})

it("keeps the SSH note out from between the auto-cleanup toggle and its threshold", () => {
  settingsQuery.data.autoCleanupEnabled = true
  const { container } = renderForm(true)

  const note = [...container.querySelectorAll("p")].find((p) => p.textContent === "preferences.orphanScanOverview.remoteLimits")
  const toggle = container.querySelector("#auto-cleanup-enabled")
  const threshold = container.querySelector("#auto-cleanup-max-files")
  if (!note || !toggle || !threshold) throw new Error("note, toggle or threshold missing")
  const follows = (a: Node, b: Node) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
  expect(follows(toggle, note) && follows(note, threshold)).toBe(false)
})
