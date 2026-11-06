/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
	formatSpeed,
	formatTrayTitle,
	MENU_BAR_ICON_PIXELS,
} from '../src/main/tray.js'

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

	it('leaves the status item title empty while idle so the Qui mark is the label', () => {
		expect(formatTrayTitle()).toBe('')
		expect(
			formatTrayTitle({
				dlSpeed: 0,
				upSpeed: 0,
				activeCount: 0,
				totalCount: 0,
			}),
		).toBe('')
	})

	it('ships the Qui mark used as the macOS status item', () => {
		const branding = path.join(
			path.dirname(fileURLToPath(import.meta.url)),
			'../src/assets/branding',
		)
		const svg = readFileSync(path.join(branding, 'menu-bar.svg'), 'utf8')
		expect(svg).toContain('M231.392 297.578')
		expect(svg).toContain('fill="white"')
		const png = readFileSync(path.join(branding, 'menu-bar.png'))
		expect(png.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a')
		expect(MENU_BAR_ICON_PIXELS).toBe(28)
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
