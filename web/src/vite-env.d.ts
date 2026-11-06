/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

declare global {
  interface Window {
    __QUI_VERSION__?: string
    /** Injected by the Qui Electron shell preload (optional). */
    quiDesktop?: {
      requestTouchId?: (reason?: string) => Promise<{ success: boolean; error?: string }>
      setWindowMaterial?: (glass: boolean) => Promise<void>
      setSidebarChrome?: (visible: boolean) => Promise<void>
      saveAccount?: (username: string, password: string) => Promise<void>
      hasSavedAccount?: () => Promise<boolean>
      unlockAccount?: () => Promise<{ username: string; password: string } | null>
    }
  }

  interface ImportMetaEnv {
    readonly VITE_DEMO?: string
  }
}

export {}
