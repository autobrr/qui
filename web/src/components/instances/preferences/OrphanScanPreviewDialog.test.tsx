/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { OrphanScanFile } from "@/types"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

// The dialog must route modifiedAt through the preference-aware formatter,
// not the browser-locale toLocaleString().
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatISOTimestamp: (iso: string) => `pref:${iso}` }),
}))

// Stable singletons: a fresh object per render would retrigger the page-merge effect forever.
const { runQuery, confirmMutation } = vi.hoisted(() => {
  const files: OrphanScanFile[] = [
    { id: 1, runId: 1, filePath: "/data/a.mkv", fileSize: 10, status: "pending", modifiedAt: "2026-01-02T03:04:05Z" },
    { id: 2, runId: 1, filePath: "/data/b.mkv", fileSize: 20, status: "pending" },
  ]
  return { runQuery: { data: { files } }, confirmMutation: { isPending: false } }
})

vi.mock("@/hooks/useOrphanScan", () => ({
  useOrphanScanRun: () => runQuery,
  useConfirmOrphanScanDeletion: () => confirmMutation,
}))

import { OrphanScanPreviewDialog } from "@/components/instances/preferences/OrphanScanPreviewDialog"

beforeEach(() => {
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

it("formats the Modified column with the date/time preferences", () => {
  render(
    <TooltipProvider>
      <OrphanScanPreviewDialog open onOpenChange={() => {}} instanceId={1} runId={1} />
    </TooltipProvider>
  )

  const cells = [...document.body.querySelectorAll("tbody td:nth-child(3)")].map((td) => td.textContent)
  expect(cells).toEqual(["pref:2026-01-02T03:04:05Z", "-"])
})
