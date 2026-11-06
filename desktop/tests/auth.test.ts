/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import {
	BiometricAuthService,
	type BiometricExecutor,
} from '../src/main/auth.js'

describe('BiometricAuthService', () => {
	it('reports supported when executor returns true', async () => {
		const mock: BiometricExecutor = {
			canEvaluate: async () => true,
			evaluate: async () => true,
		}
		const service = new BiometricAuthService(mock)
		expect(await service.isSupported()).toBe(true)
	})

	it('returns success: true when user authenticates with fingerprint', async () => {
		const mock: BiometricExecutor = {
			canEvaluate: async () => true,
			evaluate: async () => true,
		}
		const service = new BiometricAuthService(mock)
		const result = await service.authenticate('Log in to Qui')

		expect(result.success).toBe(true)
		expect(result.error).toBeUndefined()
	})

	it('returns error message when biometric evaluation fails or is cancelled', async () => {
		const mock: BiometricExecutor = {
			canEvaluate: async () => true,
			evaluate: async () => false,
		}
		const service = new BiometricAuthService(mock)
		const result = await service.authenticate('Log in to Qui')

		expect(result.success).toBe(false)
		expect(result.error).toContain('Authentication failed')
	})

	it('returns unsupported error when biometrics cannot be evaluated', async () => {
		const mock: BiometricExecutor = {
			canEvaluate: async () => false,
			evaluate: async () => false,
		}
		const service = new BiometricAuthService(mock)
		const result = await service.authenticate('Log in to Qui')

		expect(result.success).toBe(false)
		expect(result.error).toContain('not available')
	})
})
