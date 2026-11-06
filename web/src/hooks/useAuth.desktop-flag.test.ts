/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { PENDING_DESKTOP_BIOMETRIC_SESSION_KEY } from "@/lib/desktop-biometric-intent"

describe("markPendingDesktopBiometricGate (BR-web-01)", () => {
  afterEach(() => {
    vi.restoreAllMocks()
    delete (window as { quiDesktop?: unknown }).quiDesktop
  })

  it("sets session storage when Electron preload injected requestTouchId", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem")
    window.quiDesktop = { requestTouchId: async () => ({ success: true }) }

    const { markPendingDesktopBiometricGate } = await import("@/lib/desktop-biometric-intent")
    markPendingDesktopBiometricGate()

    expect(setItem).toHaveBeenCalledWith(PENDING_DESKTOP_BIOMETRIC_SESSION_KEY, "1")
  })

  it("does nothing when not running in the desktop shell", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem")

    const { markPendingDesktopBiometricGate } = await import("@/lib/desktop-biometric-intent")
    markPendingDesktopBiometricGate()

    expect(setItem).not.toHaveBeenCalled()
  })
})
