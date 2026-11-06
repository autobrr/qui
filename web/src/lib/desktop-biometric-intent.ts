/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/** Set by `useAuth` after successful login/setup in the Electron shell so `DesktopBiometricGate` runs once. */
export const PENDING_DESKTOP_BIOMETRIC_SESSION_KEY = "qui-desktop-pending-biometric"

/**
 * When embedded in the desktop app, mark that the next authenticated view should
 * prompt for Touch ID. No-op in a normal browser.
 */
export function markPendingDesktopBiometricGate(): void {
  try {
    if (typeof window !== "undefined" && window.quiDesktop?.requestTouchId) {
      sessionStorage.setItem(PENDING_DESKTOP_BIOMETRIC_SESSION_KEY, "1")
    }
  } catch {
    // Ignore storage errors (private mode)
  }
}
