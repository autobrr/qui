/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Tell the Electron shell whether the glass switch is on. No-op in a browser.
 */
export async function syncDesktopWindowMaterial(glass: boolean): Promise<void> {
  const setWindowMaterial = window.quiDesktop?.setWindowMaterial
  if (!setWindowMaterial) {
    return
  }
  await setWindowMaterial(glass)
}
