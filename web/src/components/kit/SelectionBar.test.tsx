/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"

import { SelectionBar } from "./SelectionBar"

afterEach(cleanup)

function renderBar() {
  const onClear = vi.fn()
  render(
    <>
      <input aria-label="Filter" />
      <div onKeyDown={event => event.preventDefault()}>
        <button type="button">Menu item</button>
      </div>
      <div role="dialog">
        <button type="button">Dialog button</button>
      </div>
      <SelectionBar label="Selected rows" summary="2 selected" onClear={onClear} />
    </>
  )
  return onClear
}

describe("SelectionBar", () => {
  it("clears the selection on Escape, unless a text field, a dialog, or a menu takes the key, and stops when the bar goes away", () => {
    const onClear = renderBar()

    fireEvent.keyDown(document.body, { key: "Enter" })
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Filter" }), { key: "Escape" })
    fireEvent.keyDown(screen.getByRole("button", { name: "Dialog button" }), { key: "Escape" })
    fireEvent.keyDown(screen.getByRole("button", { name: "Menu item" }), { key: "Escape" })
    fireEvent.keyDown(document.body, { key: "Escape", isComposing: true })
    expect(onClear).not.toHaveBeenCalled()

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(onClear).toHaveBeenCalledTimes(1)

    cleanup()
    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(onClear).toHaveBeenCalledTimes(1)
  })
})
