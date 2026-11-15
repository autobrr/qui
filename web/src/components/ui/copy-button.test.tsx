/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  copy: vi.fn<(text: string) => Promise<void>>(),
  toast: { success: vi.fn(), error: vi.fn() },
  translation: { t: (key: string, opts?: { label?: string }) => (opts?.label ? `${key}:${opts.label}` : key) },
}))

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/utils")>(),
  copyTextToClipboard: mocks.copy,
}))
vi.mock("sonner", () => ({ toast: mocks.toast }))
vi.mock("react-i18next", () => ({ useTranslation: () => mocks.translation }))

import { CopyButton } from "./copy-button"

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.useRealTimers()
})

function clickCopy() {
  return act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "actions.copyValue:Save Path" }))
  })
}

describe("CopyButton", () => {
  it("copies the value, shows the toast, and shows the check until the timer ends", async () => {
    mocks.copy.mockResolvedValue()
    render(<CopyButton value="/data/show" label="Save Path" />)

    await clickCopy()

    expect(mocks.copy).toHaveBeenCalledWith("/data/show")
    expect(mocks.toast.success).toHaveBeenCalledWith("feedback.copied:Save Path")
    const button = screen.getByRole("button")
    expect(button.getAttribute("data-state")).toBe("copied")

    act(() => vi.advanceTimersByTime(1499))
    expect(button.getAttribute("data-state")).toBe("copied")
    act(() => vi.advanceTimersByTime(1))
    expect(button.getAttribute("data-state")).toBe("idle")
  })

  it("shows the error toast and no check when the copy fails", async () => {
    mocks.copy.mockRejectedValue(new Error("denied"))
    render(<CopyButton value="/data/show" label="Save Path" />)

    await clickCopy()

    expect(mocks.toast.error).toHaveBeenCalledWith("feedback.copyFailed")
    expect(mocks.toast.success).not.toHaveBeenCalled()
    expect(screen.getByRole("button").getAttribute("data-state")).toBe("idle")
  })

  it("keeps the click from reaching a clickable row", async () => {
    mocks.copy.mockResolvedValue()
    const onRowClick = vi.fn()
    render(
      <div onClick={onRowClick}>
        <CopyButton value="/data/show" label="Save Path" />
      </div>
    )

    await clickCopy()

    expect(onRowClick).not.toHaveBeenCalled()
  })
})
