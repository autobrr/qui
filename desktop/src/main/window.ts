/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { BrowserWindow } from 'electron'
import { APP_NAME } from '../shared/constants.js'
import { windowMaterialForGlass } from './window-material.js'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

/**
 * Born clear. The page paints the opaque theme until glass is on, and a
 * window created with a solid fill cannot later show the desktop.
 */
export function createMainWindow(): BrowserWindow {
	const preloadPath = path.join(__dirname, '../preload/index.js')
	const material = windowMaterialForGlass(false)

	const win = new BrowserWindow({
		width: 1280,
		height: 860,
		minWidth: 900,
		minHeight: 600,
		title: APP_NAME,
		icon: path.join(__dirname, '../assets/branding/logo.png'),
		titleBarStyle: 'hiddenInset',
		transparent: true,
		backgroundColor: material.backgroundColor,
		visualEffectState: 'active',
		show: false,
		webPreferences: {
			preload: preloadPath,
			contextIsolation: true,
			nodeIntegration: false,
			sandbox: false,
		},
	})

	return win
}

export const DEFAULT_TRAFFIC_LIGHT_POSITION = { x: 12, y: 10 }
/** Vertically centered on the 48px sidebar brand row. */
export const SIDEBAR_TRAFFIC_LIGHT_POSITION = { x: 14, y: 18 }

export function applyTrafficLights(
	win: BrowserWindow,
	sidebarVisible: boolean,
): void {
	if (process.platform !== 'darwin') {
		return
	}
	win.setWindowButtonPosition(
		sidebarVisible
			? SIDEBAR_TRAFFIC_LIGHT_POSITION
			: DEFAULT_TRAFFIC_LIGHT_POSITION,
	)
}

export function applyWindowMaterial(win: BrowserWindow, glass: boolean): void {
	const material = windowMaterialForGlass(glass)
	win.setBackgroundColor(material.backgroundColor)
	win.setVibrancy(material.vibrancy)
}
