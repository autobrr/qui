/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderForm(isRemote: boolean, hasIdentity = false) {
  return render(
    <TooltipProvider>
      <OrphanScanSettingsForm instanceId={1} isRemote={isRemote} hasIdentity={hasIdentity} />
    </TooltipProvider>
  )
}

it.each([
  { hasIdentity: false, shown: true },
  { hasIdentity: true, shown: false },
])("says a second path can be listed only when the remote instance has no identity ($hasIdentity)", ({ hasIdentity, shown }) => {
  const { container } = renderForm(true, hasIdentity)

  expect(container.textContent?.includes("preferences.orphanScanOverview.remoteNoIdentity")).toBe(shown)
  expect(container.textContent).toContain("preferences.orphanScanOverview.remoteLimits")
})

it("shows no remote note for a local instance", () => {
  const { container } = renderForm(false)

  expect(container.textContent).not.toContain("preferences.orphanScanOverview.remoteLimits")
})
