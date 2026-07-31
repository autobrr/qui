/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { render, cleanup } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SelectAllHotkey } from "./SelectAllHotkey"

describe("SelectAllHotkey", () => {
  afterEach(cleanup)

  it.each([
    { name: "Cmd+A on Mac", isMac: true, init: { key: "a", metaKey: true }, fires: true },
    { name: "Ctrl+A on non-Mac", isMac: false, init: { key: "a", ctrlKey: true }, fires: true },
    { name: "Cmd+Shift+A (Chrome tab search)", isMac: true, init: { key: "A", metaKey: true, shiftKey: true }, fires: false },
    { name: "Ctrl+Shift+A on non-Mac", isMac: false, init: { key: "A", ctrlKey: true, shiftKey: true }, fires: false },
    { name: "Cmd+Alt+A", isMac: true, init: { key: "a", metaKey: true, altKey: true }, fires: false },
    { name: "plain A without modifier", isMac: true, init: { key: "a" }, fires: false },
  ])("$name fires=$fires", ({ isMac, init, fires }) => {
    const onSelectAll = vi.fn()
    render(<SelectAllHotkey onSelectAll={onSelectAll} isMac={isMac} />)

    const event = new KeyboardEvent("keydown", { cancelable: true, ...init })
    window.dispatchEvent(event)

    expect(onSelectAll).toHaveBeenCalledTimes(fires ? 1 : 0)
    expect(event.defaultPrevented).toBe(fires)
  })
})
