/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface DesktopAccount {
  username: string
  password: string
}

/** Persist the account in the desktop shell. No-op in a browser. */
export async function saveDesktopAccount(account: DesktopAccount): Promise<void> {
  const save = window.quiDesktop?.saveAccount
  if (!save) {
    return
  }
  await save(account.username, account.password)
}

/**
 * Touch ID, then the encrypted account. Null when the shell has nothing
 * stored or the fingerprint was cancelled.
 */
export async function unlockDesktopAccount(): Promise<DesktopAccount | null> {
  const bridge = window.quiDesktop
  if (!bridge?.hasSavedAccount || !bridge.unlockAccount) {
    return null
  }
  if (!(await bridge.hasSavedAccount())) {
    return null
  }
  return bridge.unlockAccount()
}
