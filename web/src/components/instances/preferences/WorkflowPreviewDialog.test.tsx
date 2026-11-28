/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) =>
        opts && "error" in opts ? `${key}:${String(opts.error)}` : opts && "time" in opts ? `${key}:${String(opts.time)}` : key,
      i18n: { t: (key: string) => key },
    }),
  }
})
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => ({ data: undefined }) }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => ({ data: undefined }) }))
const formatters = vi.hoisted(() => ({ formatTimestamp: (timestamp: number) => `at ${timestamp}` }))
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/utils")>(),
  formatBytes: (bytes: number) => `${bytes} B`,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({ useDateTimeFormatters: () => formatters }))

import { WorkflowPreviewDialog } from "@/components/instances/preferences/WorkflowPreviewDialog"

afterEach(cleanup)

const baseProps = {
  open: true,
  onOpenChange: () => {},
  title: "title",
  description: <p>description</p>,
  preview: null,
  confirmLabel: "save",
  isConfirming: false,
}

describe("WorkflowPreviewDialog", () => {
  it.each([
    { name: "while the preview loads", props: { isInitialLoading: true } },
    { name: "after the preview fails", props: { previewError: "HTTP error status 504" } },
    { name: "with a loaded preview", props: { preview: { totalMatches: 0, examples: [] } } },
  ])("shows the dry-run toggle $name", ({ props }) => {
    const onDryRunChange = vi.fn()
    render(<WorkflowPreviewDialog {...baseProps} {...props} onConfirm={() => {}} dryRun onDryRunChange={onDryRunChange} />)

    const toggle = screen.getByRole("switch", { name: "preferences.workflowDialog.footer.dryRun" })
    expect(toggle.getAttribute("aria-checked")).toBe("true")
    fireEvent.click(toggle)
    expect(onDryRunChange).toHaveBeenCalledWith(false)
  })

  it.each([
    { name: "while the preview loads", props: { isInitialLoading: true } },
    { name: "with a loaded preview", props: { preview: { totalMatches: 0, examples: [] } } },
  ])("shows no dry-run toggle $name for a caller without one", ({ props }) => {
    render(<WorkflowPreviewDialog {...baseProps} {...props} onConfirm={() => {}} />)

    expect(screen.queryByRole("switch")).toBeNull()
  })

  it("lets the user save while the preview is still loading", () => {
    const onConfirm = vi.fn()
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={onConfirm} isInitialLoading />)

    fireEvent.click(screen.getByText("preferences.workflowPreview.saveWithoutPreview"))

    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it("says why the rule does not run on the instance", () => {
    const preview = { totalMatches: 0, examples: [], skipped: { field: "PRIVATE", requires: "qBittorrent 5.0+" } }
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={() => {}} preview={preview} />)

    expect(screen.getByText("queryBuilder.ruleSkip")).toBeTruthy()
  })

  const row = (hash: string, waitsUntil?: number) => ({
    name: hash, hash, size: 1, ratio: 1, seedingTime: 1, tracker: "tracker.example", category: "", tags: "", state: "stalledUP", addedOn: 1,
    uploaded: 0, downloaded: 0, numSeeds: 0, numComplete: 0, numLeechs: 0, numIncomplete: 0, progress: 1, availability: 0,
    timeActive: 0, lastActivity: 0, completionOn: 0, totalSize: 1, waitsUntil,
  })

  it("says for each row whether the rule deletes it now or when its wait ends", () => {
    const preview = { totalMatches: 1, waitingCount: 3, examples: [row("due"), row("waiting", 4102444800), row("tracker down", 1)] }
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={() => {}} preview={preview} onExport={() => {}} />)

    const cells = screen.getAllByRole("row").slice(1).map(tr => within(tr).getAllByRole("cell")).map(td => [td[1].textContent, td[5].textContent])
    expect(cells).toEqual([
      ["due", "preferences.workflowPreview.deletesNow"],
      ["waiting", "preferences.workflowPreview.waitsUntil:at 4102444800"],
      ["tracker down", "preferences.workflowPreview.waitsForTracker"],
    ])
    expect(screen.getByText("preferences.workflowPreview.andMoreTorrents")).toBeTruthy()
    expect(screen.getByText("preferences.workflowPreview.exportCSV")).toBeTruthy()
  })

  it("shows no deletes column when no torrent waits", () => {
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={() => {}} preview={{ totalMatches: 1, examples: [row("due")] }} />)

    expect(screen.getByText("due")).toBeTruthy()
    expect(screen.queryByText("preferences.workflowPreview.deletes")).toBeNull()
  })

  it("keeps the dialog open with the error when the preview fails", () => {
    const onConfirm = vi.fn()
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={onConfirm} previewError="HTTP error status 504" />)

    expect(screen.getByText("preferences.workflowPreview.previewFailed:HTTP error status 504")).toBeTruthy()
    expect(screen.queryByText("description")).toBeNull()

    fireEvent.click(screen.getByText("save"))

    expect(onConfirm).toHaveBeenCalledTimes(1)
  })
})
