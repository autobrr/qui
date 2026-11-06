/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PENDING_DESKTOP_BIOMETRIC_SESSION_KEY } from "@/lib/desktop-biometric-intent"
import { useEffect } from "react"

/**
 * One-shot Touch ID / fingerprint prompt after registration or login in the Electron shell.
 */
export function DesktopBiometricGate() {
  useEffect(() => {
    const bridge = window.quiDesktop?.requestTouchId
    if (!bridge) {
      return
    }
    let pending: string | null
    try {
      pending = sessionStorage.getItem(PENDING_DESKTOP_BIOMETRIC_SESSION_KEY)
    } catch {
      return
    }
    if (pending !== "1") {
      return
    }
    void bridge("Use Touch ID to open Qui").finally(() => {
      try {
        sessionStorage.removeItem(PENDING_DESKTOP_BIOMETRIC_SESSION_KEY)
      } catch {
        // Ignore storage errors
      }
    })
  }, [])
  return null
}
