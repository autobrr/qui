/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import {
	GLASS_WINDOW_BACKGROUND,
	windowMaterialForGlass,
} from '../src/main/window-material.js'

describe('Main window surface (BR-desktop-05)', () => {
	it('blurs the desktop with HUD vibrancy while glass is below 100%', () => {
		expect(windowMaterialForGlass(false)).toEqual({
			vibrancy: null,
			backgroundColor: GLASS_WINDOW_BACKGROUND,
		})
		expect(windowMaterialForGlass(true)).toEqual({
			vibrancy: 'hud',
			backgroundColor: GLASS_WINDOW_BACKGROUND,
		})
		expect(GLASS_WINDOW_BACKGROUND).toBe('#00000000')
	})
})
