/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { Schema } from 'effect'
import {
	type BiometricAuthResult,
	BiometricAuthResultSchema,
} from '../shared/types.js'

const execFileAsync = promisify(execFile)

export interface BiometricExecutor {
	canEvaluate(): Promise<boolean>
	evaluate(reason: string): Promise<boolean>
}

export class MacLocalAuthExecutor implements BiometricExecutor {
	async canEvaluate(): Promise<boolean> {
		if (process.platform !== 'darwin') {
			return false
		}
		try {
			const script =
				'import LocalAuthentication; let c = LAContext(); print(c.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil))'
			const { stdout } = await execFileAsync('swift', ['-e', script], {
				timeout: 5000,
			})
			return stdout.trim().toLowerCase() === 'true'
		} catch {
			return false
		}
	}

	async evaluate(reason: string): Promise<boolean> {
		if (process.platform !== 'darwin') {
			return false
		}
		try {
			const escapedReason = reason.replace(/"/g, '\\"')
			const script = `
import LocalAuthentication
import Foundation

let context = LAContext()
var error: NSError?

if context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) {
    let semaphore = DispatchSemaphore(value: 0)
    var success = false
    context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: "${escapedReason}") { (result, _) in
        success = result
        semaphore.signal()
    }
    _ = semaphore.wait(timeout: .distantFuture)
    exit(success ? 0 : 1)
} else {
    exit(2)
}
`
			await execFileAsync('swift', ['-e', script], { timeout: 30000 })
			return true
		} catch {
			return false
		}
	}
}

export class BiometricAuthService {
	constructor(
		private readonly executor: BiometricExecutor = new MacLocalAuthExecutor(),
	) {}

	async isSupported(): Promise<boolean> {
		return this.executor.canEvaluate()
	}

	async authenticate(reason = 'Log in to Qui'): Promise<BiometricAuthResult> {
		const supported = await this.executor.canEvaluate()
		if (!supported) {
			const result = Schema.decodeUnknownSync(BiometricAuthResultSchema)({
				success: false,
				error: 'Touch ID is not available or not supported on this device.',
			})
			return result
		}

		const success = await this.executor.evaluate(reason)
		if (success) {
			return Schema.decodeUnknownSync(BiometricAuthResultSchema)({
				success: true,
			})
		}

		return Schema.decodeUnknownSync(BiometricAuthResultSchema)({
			success: false,
			error: 'Authentication failed or was cancelled by user.',
		})
	}
}
