/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { contextBridge, ipcRenderer } from 'electron'
import type { BiometricAuthResult } from '../shared/types.js'

export interface QuiDesktopBridge {
	/** macOS Touch ID / fingerprint after web sign-in or registration (no password storage). */
	requestTouchId: (reason?: string) => Promise<BiometricAuthResult>
	/** Glass switch. Vibrancy turns on for any theme while this is true. */
	setWindowMaterial: (glass: boolean) => Promise<void>
	/** Sidebar pages center the traffic lights on the logo row. */
	setSidebarChrome: (visible: boolean) => Promise<void>
	saveAccount: (username: string, password: string) => Promise<void>
	hasSavedAccount: () => Promise<boolean>
	/** Touch ID, then the encrypted account. Null when cancelled or missing. */
	unlockAccount: () => Promise<{ username: string; password: string } | null>
}

const bridge: QuiDesktopBridge = {
	requestTouchId: (reason?: string) =>
		ipcRenderer.invoke('auth:biometrics', reason),
	setWindowMaterial: (glass: boolean) =>
		ipcRenderer.invoke('window:set-material', glass),
	setSidebarChrome: (visible: boolean) =>
		ipcRenderer.invoke('window:set-sidebar', visible),
	saveAccount: (username: string, password: string) =>
		ipcRenderer.invoke('account:save', username, password),
	hasSavedAccount: () => ipcRenderer.invoke('account:has'),
	unlockAccount: () => ipcRenderer.invoke('account:unlock'),
}

contextBridge.exposeInMainWorld('quiDesktop', bridge)
