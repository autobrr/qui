/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { contextBridge, ipcRenderer } from 'electron'
import type { BiometricAuthResult } from '../shared/types.js'

export interface QuiDesktopBridge {
	/** macOS Touch ID / fingerprint after web sign-in or registration (no password storage). */
	requestTouchId: (reason?: string) => Promise<BiometricAuthResult>
}

const bridge: QuiDesktopBridge = {
	requestTouchId: (reason?: string) =>
		ipcRenderer.invoke('auth:biometrics', reason),
}

contextBridge.exposeInMainWorld('quiDesktop', bridge)
