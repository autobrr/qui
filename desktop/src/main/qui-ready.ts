/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const DEFAULT_QUI_READY_MAX_MS = 120_000
export const DEFAULT_QUI_READY_POLL_MS = 400

export interface WaitForQuiHttpReadyOptions {
	/** e.g. `http://localhost:7476` */
	baseUrl: string
	maxWaitMs?: number
	pollMs?: number
	fetchFn?: typeof fetch
	nowMs?: () => number
	sleepMs?: (ms: number) => Promise<void>
}

/**
 * Poll until Qui serves HTTP (container "running" is not enough for the API/UI).
 * Uses GET `/api/version`. Any HTTP response counts, including 428 when
 * initial setup is still required. Only a failed connection keeps polling.
 */
export async function waitForQuiHttpReady(
	options: WaitForQuiHttpReadyOptions,
): Promise<boolean> {
	const {
		baseUrl,
		maxWaitMs = DEFAULT_QUI_READY_MAX_MS,
		pollMs = DEFAULT_QUI_READY_POLL_MS,
		fetchFn = globalThis.fetch,
		nowMs = () => Date.now(),
		sleepMs = ms => new Promise<void>(resolve => setTimeout(resolve, ms)),
	} = options

	const origin = baseUrl.replace(/\/$/, '')
	const versionUrl = `${origin}/api/version`
	const deadline = nowMs() + maxWaitMs

	while (nowMs() < deadline) {
		try {
			const res = await fetchFn(versionUrl, {
				method: 'GET',
				signal: AbortSignal.timeout(2000),
			})
			if (res.status > 0) {
				return true
			}
		} catch {
			// Keep polling until deadline
		}
		await sleepMs(pollMs)
	}

	return false
}

export type WrappedQuiOpenResult = 'opened' | 'unavailable'

/**
 * Start Docker, wait until Qui answers HTTP, then open the real web UI.
 * A status line is shown first so the window is not an empty vibrancy shell
 * while containers are still stopped.
 */
export async function openWrappedQui(steps: {
	showStatus: (message: string, processInfo?: string) => Promise<void>
	ensureContainers: () => Promise<void>
	waitForHttp: () => Promise<boolean>
	openWeb: () => Promise<void>
	unavailableMessage: string
}): Promise<WrappedQuiOpenResult> {
	await steps.showStatus(
		'Starting Qui…',
		'Ensuring Docker containers (qui, qbit) are running…',
	)
	await steps.ensureContainers()
	await steps.showStatus(
		'Starting Qui…',
		'Waiting for Qui HTTP service at local port…',
	)
	const ready = await steps.waitForHttp()
	if (!ready) {
		await steps.showStatus(
			steps.unavailableMessage,
			'Connection timed out waiting for Qui HTTP API.',
		)
		return 'unavailable'
	}
	await steps.openWeb()
	return 'opened'
}
