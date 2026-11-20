/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useTorrentListEscape, type UseTorrentListEscapeParams } from "./useTorrentListEscape"

type State = Pick<UseTorrentListEscapeParams, "hasSelection" | "hasSearch">

function setup(initial: State) {
  const clearSelection = vi.fn()
  const clearSearch = vi.fn()
  const hook = renderHook((state: State) => useTorrentListEscape({ ...state, clearSelection, clearSearch }), { initialProps: initial })
  return { ...hook, clearSelection, clearSearch }
}

const pressEscape = (target: EventTarget = document.body) =>
  target.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }))

afterEach(() => {
  cleanup()
  document.body.innerHTML = ""
})

describe("useTorrentListEscape", () => {
  // regression: #4260
  it("closes the details first, then clears the search on the next Escape", () => {
    const { rerender, clearSelection, clearSearch } = setup({ hasSelection: true, hasSearch: true })

    pressEscape()
    expect(clearSelection).toHaveBeenCalledOnce()
    expect(clearSearch).not.toHaveBeenCalled()

    rerender({ hasSelection: false, hasSearch: true })
    pressEscape()
    expect(clearSearch).toHaveBeenCalledOnce()
  })

  it("keeps the search while the user types in another text field", () => {
    const { clearSearch } = setup({ hasSelection: false, hasSearch: true })
    const field = document.body.appendChild(document.createElement("input"))

    pressEscape(field)

    expect(clearSearch).not.toHaveBeenCalled()
  })
})
