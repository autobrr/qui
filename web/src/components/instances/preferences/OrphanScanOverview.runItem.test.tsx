/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render } from "@testing-library/react"
import type { OrphanScanRun } from "@/types"

// OrphanScanRunItem only needs a translator; keep the module's other exports
// (initReactI18next is pulled in through the import graph).
vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) =>
        opts && "total" in opts ? `${key}:${String(opts.total)}` : key,
    }),
  }
})

import { OrphanScanRunItem } from "@/components/instances/preferences/OrphanScanOverview"

function makeRun(overrides: Partial<OrphanScanRun> = {}): OrphanScanRun {
  return {
    id: 1,
    instanceId: 1,
    status: "completed",
    triggeredBy: "manual",
    scanPaths: [],
    filesFound: 0,
    filesDeleted: 0,
    foldersDeleted: 0,
    bytesReclaimed: 0,
    truncated: false,
    startedAt: "2026-01-01T00:00:00Z",
    completedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  }
}

afterEach(cleanup)

describe("OrphanScanRunItem", () => {
  const paths = ["/data/torrents/movies", "/data/torrents/cross-seed/MTV"]

  it("expands a run that only has scan paths, listing them with a count", () => {
    const { container } = render(<OrphanScanRunItem run={makeRun({ scanPaths: paths })} />)

    fireEvent.click(container.querySelector("button")!)

    expect(container.textContent).toContain("preferences.orphanScanOverview.scannedPaths:2")
    expect(container.textContent).toContain(paths.join("\n"))
  })

  it("renders a run with no error and no scan paths as a plain row", () => {
    const { container } = render(<OrphanScanRunItem run={makeRun()} />)

    // No trigger means no chevron and nothing to expand.
    expect(container.querySelector("button")).toBeNull()
  })
})
