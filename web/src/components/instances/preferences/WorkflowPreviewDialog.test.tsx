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
  it("lets the user save while the preview is still loading", () => {
    const onConfirm = vi.fn()
    render(<WorkflowPreviewDialog {...baseProps} onConfirm={onConfirm} isInitialLoading />)

    fireEvent.click(screen.getByText("preferences.workflowPreview.saveWithoutPreview"))

    expect(onConfirm).toHaveBeenCalledTimes(1)
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
