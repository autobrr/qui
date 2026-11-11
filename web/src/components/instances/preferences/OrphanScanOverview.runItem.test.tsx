/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render as rtlRender, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactElement } from "react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { OrphanScanRun } from "@/types"

const { getOrphanScanRun } = vi.hoisted(() => ({ getOrphanScanRun: vi.fn() }))
vi.mock("@/lib/api", () => ({ api: { getOrphanScanRun } }))

function render(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { client, ...rtlRender(<QueryClientProvider client={client}>{ui}</QueryClientProvider>) }
}

// OrphanScanRunItem only needs a translator; keep the module's other exports
// (initReactI18next is pulled in through the import graph).
vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) => {
        const value = opts?.total ?? opts?.count ?? opts?.deleted
        const size = opts?.size === undefined ? "" : `:${String(opts.size)}`
        return value === undefined ? key : `${key}:${String(value)}${size}`
      },
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
    scanPathCount: 0,
    filesFound: 0,
    filesDeleted: 0,
    foldersDeleted: 0,
    bytesReclaimed: 0,
    truncated: false,
    startedAt: "2026-01-01T00:00:00Z",
    completedAt: "2026-01-01T00:00:00Z",
    filesystemMode: "local",
    identityComplete: true,
    ...overrides,
  }
}

afterEach(() => {
  cleanup()
  getOrphanScanRun.mockReset()
  vi.useRealTimers()
})

describe("OrphanScanRunItem", () => {
  const paths = ["/data/torrents/movies", "/data/torrents/cross-seed/MTV"]

  const copyButton = (container: HTMLElement) => container.querySelector("[aria-label='actions.copyValue']")

  it("expands a run that only has scan paths, loading them once after it opens", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    getOrphanScanRun.mockResolvedValue({ ...makeRun({ status: "scanning", scanPathCount: 2 }), scanPaths: paths, files: [] })
    const { client, container } = render(<OrphanScanRunItem run={makeRun({ status: "scanning", scanPathCount: 2 })} />)
    expect(getOrphanScanRun).not.toHaveBeenCalled()

    fireEvent.click(container.querySelector("button")!)

    expect(container.textContent).toContain("preferences.orphanScanOverview.scannedPaths:2")
    expect(container.textContent).toContain("preferences.orphanScanOverview.scanPathsLoading")
    expect(copyButton(container)).toBeNull()
    await waitFor(() => expect(container.textContent).toContain(paths.join("\n")))
    expect(container.textContent).not.toContain("preferences.orphanScanOverview.scanPathsLoading")
    expect(copyButton(container)).not.toBeNull()

    fireEvent.click(container.querySelector("button")!)
    fireEvent.click(container.querySelector("button")!)
    // A run status event and a stream reconnect invalidate these prefixes.
    await client.invalidateQueries({ queryKey: ["orphan-scan", 1] })
    await client.invalidateQueries({ queryKey: ["orphan-scan"] })
    await vi.advanceTimersByTimeAsync(5_000)
    expect(getOrphanScanRun).toHaveBeenCalledTimes(1)
    expect(getOrphanScanRun).toHaveBeenCalledWith(1, 1, { limit: 1 })
  })

  it("shows an error line when the scan paths fail to load", async () => {
    getOrphanScanRun.mockRejectedValue(new Error("boom"))
    const { container } = render(<OrphanScanRunItem run={makeRun({ scanPathCount: 2 })} />)

    fireEvent.click(container.querySelector("button")!)

    await waitFor(() => expect(container.textContent).toContain("preferences.orphanScanOverview.scanPathsLoadFailed"))
    expect(container.textContent).toContain("preferences.orphanScanOverview.scannedPaths:2")
    expect(container.textContent).not.toContain("preferences.orphanScanOverview.scanPathsLoading")
    expect(copyButton(container)).toBeNull()
  })

  it("keeps the reclaimed size's number and unit together", () => {
    const { container } = render(<OrphanScanRunItem run={makeRun({ filesFound: 7, filesDeleted: 7, bytesReclaimed: 9.2 * 1024 ** 3 })} />)

    expect(container.textContent).toMatch(/9\.2\u00a0GiB/)
  })

  it("renders a run with no error and no scan paths as a plain row", () => {
    const { container } = render(<OrphanScanRunItem run={makeRun()} />)

    // No trigger means no chevron and nothing to expand.
    expect(container.querySelector("button")).toBeNull()
  })
  it.each([
    { status: "completed" as const, filesFound: 0 },
    { status: "preview_ready" as const, filesFound: 1 },
  ])("identifies a partial $status run before expanding it", ({ status, filesFound }) => {
    const warning = "Partial scan: /data/missing is unavailable"
    const { container } = render(
      <TooltipProvider>
        <OrphanScanRunItem run={makeRun({ status, filesFound, partial: true, errorMessage: warning })} />
      </TooltipProvider>
    )

    expect(container.textContent).toContain("preferences.orphanScanOverview.statusPartial")
    expect(container.textContent).not.toContain("preferences.orphanScanOverview.statusClean")
    fireEvent.click(container.querySelector("button")!)
    expect(container.textContent).toContain(warning)
  })

  it.each([
    { filesDeleted: 9, foldersDeleted: 3, want: ["deletedFiles:9", "deletedDirs:3"], omit: [] },
    { filesDeleted: 9, foldersDeleted: 0, want: ["deletedFiles:9"], omit: ["deletedDirs"] },
    { filesDeleted: 0, foldersDeleted: 3, want: ["deletedDirs:3"], omit: ["deletedFiles"] },
    { filesDeleted: 0, foldersDeleted: 0, want: ["deletedFiles:0"], omit: ["deletedDirs"] },
  ])("names $filesDeleted files and $foldersDeleted directories in the deleted stats", ({ filesDeleted, foldersDeleted, want, omit }) => {
    const { container } = render(<OrphanScanRunItem run={makeRun({ filesFound: 12, filesDeleted, foldersDeleted })} />)

    for (const fragment of want) expect(container.textContent).toContain(`preferences.orphanScanOverview.${fragment}`)
    for (const fragment of omit) expect(container.textContent).not.toContain(`preferences.orphanScanOverview.${fragment}`)
  })

  it("keeps a failed run failed when its scan was partial", () => {
    const { container } = render(<OrphanScanRunItem run={makeRun({ status: "failed", partial: true, errorMessage: "Deletion failed" })} />)
    expect(container.textContent).toContain("preferences.orphanScanOverview.statusFailed")
    expect(container.textContent).not.toContain("preferences.orphanScanOverview.statusPartial")
  })

})
