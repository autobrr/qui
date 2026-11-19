/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { hasOnlyMod, latinKeyOf } from "../keyboard"

const NO_MODIFIER = { metaKey: false, ctrlKey: false, altKey: false, shiftKey: false }

describe("hasOnlyMod", () => {
  it("takes ⌘ on macOS and Ctrl on Windows and Linux", () => {
    const command = { ...NO_MODIFIER, metaKey: true }
    const control = { ...NO_MODIFIER, ctrlKey: true }

    expect(hasOnlyMod(command, true)).toBe(true)
    expect(hasOnlyMod(control, true)).toBe(false)
    expect(hasOnlyMod(control, false)).toBe(true)
    expect(hasOnlyMod(command, false)).toBe(false)
  })

  it("leaves AltGr alone, which reports Ctrl+Alt on Windows", () => {
    expect(hasOnlyMod({ ...NO_MODIFIER, ctrlKey: true, altKey: true }, false)).toBe(false)
  })

  it("leaves a shortcut with Shift to the browser", () => {
    expect(hasOnlyMod({ ...NO_MODIFIER, ctrlKey: true, shiftKey: true }, false)).toBe(false)
  })
})

describe("latinKeyOf", () => {
  it("reads the letter that the layout types", () => {
    expect(latinKeyOf({ key: "K", code: "KeyK" })).toBe("k")
    // Dvorak types "t" with the key at the place of K.
    expect(latinKeyOf({ key: "t", code: "KeyK" })).toBe("t")
  })

  it("falls back to the place of the key on a layout that types no Latin letter", () => {
    expect(latinKeyOf({ key: "л", code: "KeyK" })).toBe("k")
  })

  it("reads a digit key that types a symbol, as on AZERTY", () => {
    expect(latinKeyOf({ key: "&", code: "Digit1" })).toBe("1")
  })

  it("gives no letter for a key that is not a letter or a digit", () => {
    expect(latinKeyOf({ key: "Enter", code: "Enter" })).toBeNull()
  })
})
