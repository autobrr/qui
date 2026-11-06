/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import { MAIN_WINDOW_BACKGROUND } from '../src/main/window.js'

describe('Main window surface (BR-desktop-05)', () => {
	it('uses a transparent window chrome with macOS vibrancy so the desktop shows through behind Qui’s web UI', () => {
		expect(MAIN_WINDOW_BACKGROUND).toBe('#00000000')
	})
})
