/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"

import { ConfirmDialog } from "./ConfirmDialog"

afterEach(cleanup)

describe("ConfirmDialog", () => {
  it("stays open when the step fails, so the user can try again", async () => {
    const onOpenChange = vi.fn()
    let fail: (error: Error) => void = () => {}
    const onConfirm = () => new Promise<void>((_resolve, reject) => {
      fail = reject
    })
    render(<ConfirmDialog open onOpenChange={onOpenChange} title="Delete seedbox?" description="It goes away." confirmLabel="Delete" onConfirm={onConfirm} />)

    const confirm = screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement
    fireEvent.click(confirm)
    await waitFor(() => expect(confirm.disabled).toBe(true))
    fail(new Error("server down"))

    await waitFor(() => expect(confirm.disabled).toBe(false))
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it("names the button that closes the dialog with cancelLabel, so that a Cancel step does not get two Cancel buttons", () => {
    const onOpenChange = vi.fn()
    const onConfirm = vi.fn()
    render(<ConfirmDialog open onOpenChange={onOpenChange} title="Cancel this run?" description="It stops." confirmLabel="Cancel the run" cancelLabel="Keep running" onConfirm={onConfirm} />)

    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Keep running" }))

    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
