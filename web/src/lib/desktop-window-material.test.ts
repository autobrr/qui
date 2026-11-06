/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { syncDesktopWindowMaterial } from "./desktop-window-material"

afterEach(() => {
  delete window.quiDesktop
})

describe("syncDesktopWindowMaterial (BR-desktop-05)", () => {
  it("does nothing when the page is not inside the Electron shell", async () => {
    await syncDesktopWindowMaterial(false)
    expect(window.quiDesktop).toBeUndefined()
  })

  it("sends the glass switch to the shell", async () => {
    const calls: boolean[] = []
    window.quiDesktop = {
      setWindowMaterial: async (glass: boolean) => {
        calls.push(glass)
      },
    }

    await syncDesktopWindowMaterial(true)
    await syncDesktopWindowMaterial(false)

    expect(calls).toEqual([true, false])
  })
})
