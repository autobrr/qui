/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import { AccountVault } from '../src/main/account-vault.js'

describe('Encrypted desktop account (BR-desktop-12)', () => {
	it('round-trips the username and password through the cipher', () => {
		let stored: Uint8Array | null = null
		const vault = new AccountVault(
			{
				encrypt: plain => new TextEncoder().encode(`sealed:${plain}`),
				decrypt: bytes =>
					new TextDecoder().decode(bytes).replace(/^sealed:/, ''),
			},
			() => stored,
			bytes => {
				stored = bytes
			},
		)

		expect(vault.has()).toBe(false)
		vault.save({ username: 'ada', password: 's3cret' })
		expect(vault.has()).toBe(true)
		expect(vault.read()).toEqual({ username: 'ada', password: 's3cret' })
		expect(new TextDecoder().decode(stored ?? new Uint8Array())).toContain(
			'sealed:',
		)
	})
})
