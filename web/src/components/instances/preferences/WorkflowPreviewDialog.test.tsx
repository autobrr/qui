/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) =>
        opts && "error" in opts ? `${key}:${String(opts.error)}` : key,
      i18n: { t: (key: string) => key },
    }),
  }
})
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => ({ data: undefined }) }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => ({ data: undefined }) }))

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

  it("keeps the dialog open with the error when the preview fails", () => {
    const onConfirm = vi.fn()
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={onConfirm} previewError="HTTP error status 504" />)

    expect(screen.getByText("preferences.workflowPreview.previewFailed:HTTP error status 504")).toBeTruthy()
    expect(screen.queryByText("description")).toBeNull()

    fireEvent.click(screen.getByText("save"))

    expect(onConfirm).toHaveBeenCalledTimes(1)
  })
})
