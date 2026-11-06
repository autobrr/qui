/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import { formatSpeed, formatTrayTitle } from '../src/main/tray.js'
import { APP_NAME } from '../src/shared/constants.js'

describe('Tray speed formatting and title helpers', () => {
	it('formats 0 bytes per second', () => {
		expect(formatSpeed(0)).toBe('0 B/s')
		expect(formatSpeed(-100)).toBe('0 B/s')
	})

	it('formats KB/s rates accurately', () => {
		expect(formatSpeed(1024)).toBe('1.0 KB/s')
		expect(formatSpeed(512 * 1024)).toBe('512 KB/s')
	})

	it('formats MB/s rates accurately', () => {
		expect(formatSpeed(1024 * 1024)).toBe('1.0 MB/s')
		expect(formatSpeed(15.5 * 1024 * 1024)).toBe('15.5 MB/s')
	})

	it('formats GB/s rates accurately', () => {
		expect(formatSpeed(1024 * 1024 * 1024)).toBe('1.0 GB/s')
	})

	it('returns capitalized APP_NAME ("Qui") when idle without transfer stats', () => {
		expect(formatTrayTitle()).toBe(APP_NAME)
		expect(
			formatTrayTitle({
				dlSpeed: 0,
				upSpeed: 0,
				activeCount: 0,
				totalCount: 0,
			}),
		).toBe('Qui')
	})

	it('formats tray title with dynamic download and upload speeds', () => {
		const title = formatTrayTitle({
			dlSpeed: 2.5 * 1024 * 1024,
			upSpeed: 500 * 1024,
			activeCount: 3,
			totalCount: 12,
		})
		expect(title).toBe('↓ 2.5 MB/s ↑ 500 KB/s')
	})
})
