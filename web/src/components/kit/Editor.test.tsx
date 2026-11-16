/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { createRef } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"

import { Editor } from "./Editor"

afterEach(cleanup)

describe("Editor", () => {
  it("has no form and closes with Close when it has nothing to save", () => {
    const onOpenChange = vi.fn()
    render(<Editor open onOpenChange={onOpenChange} title="Log entry"><form aria-label="inner" /></Editor>)

    expect(screen.getAllByRole("form", { hidden: true }).map((form) => form.getAttribute("aria-label"))).toEqual(["inner"])
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull()
    // The footer button, not the X in the corner.
    fireEvent.click(screen.getAllByRole("button", { name: "Close" }).find((button) => button.dataset.slot !== "dialog-close")!)

    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("returns focus to returnFocusTo when it closes, not to the button that had focus at open", async () => {
    const target = createRef<HTMLButtonElement>()
    const page = (open: boolean) => (
      <>
        <button ref={target}>Profile chip</button>
        <button>Menu item</button>
        <Editor open={open} onOpenChange={() => {}} title="New profile" submitLabel="Create" onSubmit={() => {}} returnFocusTo={target}>
          <input aria-label="Name" />
        </Editor>
      </>
    )
    const { rerender } = render(page(false))
    screen.getByRole("button", { name: "Menu item" }).focus()

    rerender(page(true))
    rerender(page(false))

    await waitFor(() => expect(document.activeElement).toBe(target.current))
  })
})
