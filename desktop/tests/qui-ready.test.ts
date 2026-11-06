/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import {
	DEFAULT_QUI_READY_MAX_MS,
	openWrappedQui,
	waitForQuiHttpReady,
} from '../src/main/qui-ready.js'

describe('waitForQuiHttpReady', () => {
	it('returns true when setup is still required (HTTP 428)', async () => {
		const fetchFn = async () =>
			new Response(JSON.stringify({ setup_required: true }), { status: 428 })
		const ok = await waitForQuiHttpReady({
			baseUrl: 'http://localhost:7476',
			maxWaitMs: 1000,
			pollMs: 10,
			fetchFn,
		})
		expect(ok).toBe(true)
	})

	it('returns true when /api/version responds OK', async () => {
		const fetchFn = async (url: string) => {
			expect(url).toBe('http://localhost:7476/api/version')
			return new Response('{}', { status: 200 })
		}
		const ok = await waitForQuiHttpReady({
			baseUrl: 'http://localhost:7476',
			maxWaitMs: 1000,
			pollMs: 10,
			fetchFn,
		})
		expect(ok).toBe(true)
	})

	it('polls until OK then returns true', async () => {
		let calls = 0
		const fetchFn = async () => {
			calls++
			if (calls < 3) {
				throw new Error('connection refused')
			}
			return new Response('{}', { status: 200 })
		}
		const sleepCalls: number[] = []
		const ok = await waitForQuiHttpReady({
			baseUrl: 'http://localhost:7476/',
			maxWaitMs: DEFAULT_QUI_READY_MAX_MS,
			pollMs: 5,
			fetchFn,
			sleepMs: async ms => {
				sleepCalls.push(ms)
			},
		})
		expect(ok).toBe(true)
		expect(calls).toBe(3)
		expect(sleepCalls.length).toBe(2)
	})

	it('returns false when deadline passes without success', async () => {
		let now = 0
		const fetchFn = async () => {
			throw new Error('refused')
		}
		const ok = await waitForQuiHttpReady({
			baseUrl: 'http://localhost:7476',
			maxWaitMs: 50,
			pollMs: 10,
			fetchFn,
			nowMs: () => now,
			sleepMs: async ms => {
				now += ms
			},
		})
		expect(ok).toBe(false)
	})
})

describe('openWrappedQui', () => {
	it('shows status, starts containers, waits for HTTP, then opens the web UI', async () => {
		const order: string[] = []
		const result = await openWrappedQui({
			showStatus: async message => {
				order.push(`status:${message}`)
			},
			ensureContainers: async () => {
				order.push('ensure')
			},
			waitForHttp: async () => {
				order.push('wait')
				return true
			},
			openWeb: async () => {
				order.push('open')
			},
			unavailableMessage: 'Qui is not responding',
		})
		expect(result).toBe('opened')
		expect(order).toEqual([
			'status:Starting Qui…',
			'ensure',
			'status:Starting Qui…',
			'wait',
			'open',
		])
	})

	it('does not open the web UI when HTTP never becomes ready', async () => {
		const order: string[] = []
		const result = await openWrappedQui({
			showStatus: async message => {
				order.push(message)
			},
			ensureContainers: async () => {
				order.push('ensure')
			},
			waitForHttp: async () => false,
			openWeb: async () => {
				order.push('open')
			},
			unavailableMessage: 'Qui is not responding',
		})
		expect(result).toBe('unavailable')
		expect(order).toEqual([
			'Starting Qui…',
			'ensure',
			'Starting Qui…',
			'Qui is not responding',
		])
	})
})
