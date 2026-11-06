/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { saveDesktopAccount, unlockDesktopAccount } from "./desktop-account"

afterEach(() => {
  delete window.quiDesktop
})

describe("desktop account bridge (BR-desktop-12)", () => {
  it("does nothing in a normal browser", async () => {
    await saveDesktopAccount({ username: "ada", password: "secret" })
    expect(await unlockDesktopAccount()).toBeNull()
  })

  it("stores the account and returns it after fingerprint unlock", async () => {
    let saved: { username: string; password: string } | null = null
    window.quiDesktop = {
      saveAccount: async (username, password) => {
        saved = { username, password }
      },
      hasSavedAccount: async () => saved !== null,
      unlockAccount: async () => saved,
    }

    await saveDesktopAccount({ username: "ada", password: "secret" })
    expect(await unlockDesktopAccount()).toEqual({
      username: "ada",
      password: "secret",
    })
  })

  it("returns null when fingerprint unlock yields no account", async () => {
    window.quiDesktop = {
      hasSavedAccount: async () => true,
      unlockAccount: async () => null,
    }
    expect(await unlockDesktopAccount()).toBeNull()
  })
})
