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
  it.each([[true, true], [false, false]])("holds the confirm button off while confirmDisabled is %s", (confirmDisabled, disabled) => {
    render(
      <ConfirmDialog open onOpenChange={() => {}} title="Delete seedbox?" description="It goes away." confirmLabel="Delete" onConfirm={() => {}} confirmDisabled={confirmDisabled} />
    )

    expect((screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(disabled)
    expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(false)
  })

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
})
