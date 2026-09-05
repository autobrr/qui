/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DiscScans } from "@/hooks/useDiscScans"
import type { DiscScanRun } from "@/types"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { DiscReportButton, TorrentDiscReportDialog } from "./TorrentDiscReportDialog"

const { toast, copyTextToClipboard } = vi.hoisted(() => ({
  toast: { success: vi.fn(), error: vi.fn() },
  copyTextToClipboard: vi.fn(),
}))
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string, options?: Record<string, unknown>) => options ? `${key}:${Object.values(options).join(",")}` : key }),
}))
vi.mock("sonner", () => ({ toast }))
vi.mock("@/lib/utils", async (importOriginal) => ({ ...(await importOriginal<object>()), copyTextToClipboard }))

// The tabs indicator measures itself through ResizeObserver, which jsdom lacks.
vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function run(overrides: Partial<DiscScanRun>): DiscScanRun {
  return {
    id: 7, instanceId: 1, torrentHash: "abc", discPath: "Set/Disc 1", resolvedPath: "/data/Set/Disc 1", status: "completed",
    processedBytes: 0, totalBytes: 0, createdAt: "2026-01-01T00:00:00Z", ...overrides,
  }
}

function scansWith(current?: DiscScanRun, startState: Partial<DiscScans["start"]> = {}): DiscScans {
  const start = { mutate: vi.fn(), reset: vi.fn(), isError: false, isPending: false, error: null, data: undefined, variables: undefined, ...startState } as Partial<DiscScans["start"]> & { data?: DiscScanRun; variables?: { discPath: string; force: boolean } }
  const cancel = { mutate: vi.fn(), isPending: false }
  const runsByPath = new Map(current ? [[current.discPath, current]] : [])
  return {
    runsByPath,
    runFor: (discPath: string) => runsByPath.get(discPath) ?? (start.variables?.discPath === discPath ? start.data : undefined),
    start: start as unknown as DiscScans["start"],
    cancel: cancel as unknown as DiscScans["cancel"],
  }
}

describe("DiscReportButton", () => {
  it("labels the states: no report, active, cached, failed", () => {
    const labels = ([undefined, "pending", "scanning", "completed", "failed", "canceled"] as const).map((status) => {
      const { getByRole, unmount } = render(<DiscReportButton status={status} onClick={() => {}} />)
      const label = getByRole("button").getAttribute("aria-label")
      unmount()
      return label
    })
    expect(labels).toEqual([
      "discScan.action",
      "discScan.actionActive",
      "discScan.actionActive",
      "discScan.actionReady",
      "discScan.actionFailed",
      "discScan.action",
    ])
  })

  it("does not fire while disabled", () => {
    const onClick = vi.fn()
    const { getByRole } = render(<DiscReportButton disabled onClick={onClick} />)
    fireEvent.click(getByRole("button"))
    expect(onClick).not.toHaveBeenCalled()
  })
})

describe("TorrentDiscReportDialog", () => {
  it("copies the Quick Summary, then the Forum block after switching tabs", async () => {
    copyTextToClipboard.mockResolvedValue(undefined)
    const current = run({ quickSummary: "QUICK", forumsBlock: "[b]FORUM[/b]" })
    const { getByText, getByRole } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scansWith(current)} />)

    expect(getByText("QUICK").tagName).toBe("PRE")
    fireEvent.click(getByText("discScan.copyQuickSummary"))
    await waitFor(() => expect(copyTextToClipboard).toHaveBeenCalledWith("QUICK"))
    expect(toast.success).toHaveBeenCalledWith("discScan.toast.copied:discScan.copyQuickSummary")

    fireEvent.mouseDown(getByRole("tab", { name: "discScan.forum" }))
    fireEvent.click(getByText("discScan.copyForum"))
    await waitFor(() => expect(copyTextToClipboard).toHaveBeenCalledWith("[b]FORUM[/b]"))
  })

  it("shows only the Quick Summary on a phone and copies the Forum block from a button", async () => {
    copyTextToClipboard.mockResolvedValue(undefined)
    const matchMedia = window.matchMedia
    window.matchMedia = vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })
    try {
      const current = run({ quickSummary: "QUICK", forumsBlock: "[b]FORUM[/b]" })
      const { getByText, queryByRole, queryByText } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scansWith(current)} />)

      expect(getByText("QUICK").tagName).toBe("PRE")
      expect(queryByText("[b]FORUM[/b]")).toBeNull()
      expect(queryByRole("tab")).toBeNull()
      fireEvent.click(getByText("discScan.copyForum"))
      await waitFor(() => expect(copyTextToClipboard).toHaveBeenCalledWith("[b]FORUM[/b]"))
    } finally {
      window.matchMedia = matchMedia
    }
  })

  it("shows the cached report of another torrent from the start response when the list has no row", () => {
    const shared = run({ torrentHash: "other", discPath: "Movie-GRP", quickSummary: "SHARED" })
    const scans = scansWith(undefined, { data: shared, variables: { discPath: "Movie-OTHER", force: false } } as Partial<DiscScans["start"]>)
    const { getByText } = render(<TorrentDiscReportDialog discPath="Movie-OTHER" onClose={() => {}} scans={scans} />)
    expect(getByText("SHARED").tagName).toBe("PRE")
  })

  it("rescans with force from a cached report", () => {
    const current = run({ quickSummary: "QUICK" })
    const scans = scansWith(current)
    const { getByText } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scans} />)
    fireEvent.click(getByText("discScan.rescan"))
    expect(scans.start.mutate).toHaveBeenCalledWith({ discPath: "Set/Disc 1", force: true })
  })

  it("shows the queue position and cancels a queued run", () => {
    const current = run({ status: "pending", queuePosition: 3 })
    const scans = scansWith(current)
    const { getByText } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scans} />)
    expect(getByText("discScan.queued:3")).toBeTruthy()
    fireEvent.click(getByText("discScan.cancel"))
    expect(scans.cancel.mutate).toHaveBeenCalledWith(7)
  })

  it("shows byte progress while scanning", () => {
    const current = run({ status: "scanning", processedBytes: 25 * 1024 ** 3, totalBytes: 50 * 1024 ** 3 })
    const { getByText, getByRole } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scansWith(current)} />)
    expect(getByText("discScan.scanning:25 GiB,50 GiB")).toBeTruthy()
    expect(getByText("50%")).toBeTruthy()
    expect(getByRole("progressbar")).toBeTruthy()
  })

  it("shows the error of a failed run and offers Rescan", () => {
    const current = run({ status: "failed", errorMessage: "no BDMV/PLAYLIST directory" })
    const scans = scansWith(current)
    const { getByText, queryByRole } = render(<TorrentDiscReportDialog discPath={current.discPath} onClose={() => {}} scans={scans} />)
    expect(getByText("no BDMV/PLAYLIST directory")).toBeTruthy()
    expect(queryByRole("tab")).toBeNull()
    fireEvent.click(getByText("discScan.rescan"))
    expect(scans.start.mutate).toHaveBeenCalledWith({ discPath: "Set/Disc 1", force: true })
  })

  it("shows the start error when the server refuses the scan", () => {
    const scans = scansWith(undefined, { isError: true, error: new Error("Instance does not have filesystem access") } as Partial<DiscScans["start"]>)
    const { getByText } = render(<TorrentDiscReportDialog discPath="Set/Disc 1" onClose={() => {}} scans={scans} />)
    expect(getByText("Instance does not have filesystem access")).toBeTruthy()
    fireEvent.click(getByText("discScan.retry"))
    expect(scans.start.mutate).toHaveBeenCalledWith({ discPath: "Set/Disc 1", force: false })
  })
})
