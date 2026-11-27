/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { createRef } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"

import { Editor } from "./Editor"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function stubPointer(pointer: "fine" | "coarse") {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query === `(pointer: ${pointer})`,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}

describe("Editor", () => {
  it("has no form and closes with Close when it has nothing to save", () => {
    const onOpenChange = vi.fn()
    render(<Editor open onOpenChange={onOpenChange} title="Log entry"><form aria-label="inner" /></Editor>)

    expect(screen.getAllByRole("form", { hidden: true }).map((form) => form.getAttribute("aria-label"))).toEqual(["inner"])
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull()
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

  it.each([["fine", true], ["coarse", false]] as const)("with a %s pointer, focuses the first field on open: %s", async (pointer, focusesField) => {
    stubPointer(pointer)
    render(
      <Editor open onOpenChange={() => {}} title="New profile" submitLabel="Create" onSubmit={() => {}}>
        <input aria-label="Name" />
      </Editor>
    )

    const dialog = await screen.findByRole("dialog")
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true))
    expect(document.activeElement === screen.getByRole("textbox", { name: "Name" })).toBe(focusesField)
  })

  it.each([
    ["skips a disabled combobox and focuses the first field that takes input", <button type="button" role="combobox" aria-label="Type" disabled />, "Name"],
    ["focuses the editor itself when the field it picks takes no focus", <div role="combobox" aria-label="Type" />, null],
  ] as const)("with a fine pointer, %s", async (_case, leading, focused) => {
    stubPointer("fine")
    render(
      <Editor open onOpenChange={() => {}} title="New target" submitLabel="Create" onSubmit={() => {}}>
        {leading}
        {focused === null ? null : <input aria-label={focused} />}
      </Editor>
    )

    const dialog = await screen.findByRole("dialog")
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true))
    expect(document.activeElement).toBe(focused === null ? dialog : screen.getByRole("textbox", { name: focused }))
  })

  it.each([["fine", "Star"], ["coarse", null]] as const)("with a %s pointer and initialFocus, focuses the element it names: %s", async (pointer, focused) => {
    stubPointer(pointer)
    render(
      <Editor open onOpenChange={() => {}} title="Icon for main" initialFocus="[data-current]">
        <input aria-label="Letters" />
        <button type="button" data-current>Star</button>
      </Editor>
    )

    const dialog = await screen.findByRole("dialog")
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true))
    expect(document.activeElement).toBe(focused === null ? dialog : screen.getByRole("button", { name: focused }))
  })

  it("puts the pinned content after the title, outside the fields that scroll", () => {
    render(
      <Editor open onOpenChange={() => {}} title="Icon for main" submitLabel="Save" onSubmit={() => {}} pinned={<p>Preview</p>}>
        <input aria-label="Letters" />
      </Editor>
    )

    const preview = screen.getByText("Preview")
    expect(preview.closest("form")).toBeNull()
    expect(screen.getByRole("textbox", { name: "Letters" }).closest("form")).not.toBeNull()
    expect(screen.getByRole("heading", { name: "Icon for main" }).compareDocumentPosition(preview) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})
