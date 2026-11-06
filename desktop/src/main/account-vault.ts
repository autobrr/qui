/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Schema } from 'effect'

export const SavedAccountSchema = Schema.Struct({
	username: Schema.String,
	password: Schema.String,
})

export type SavedAccount = typeof SavedAccountSchema.Type

export interface AccountCipher {
	encrypt(plain: string): Uint8Array
	decrypt(bytes: Uint8Array): string
}

/**
 * OS-encrypted account file. The cipher is Electron safeStorage in the app,
 * and a stand-in in tests. The password never sits in localStorage.
 */
export class AccountVault {
	constructor(
		private readonly cipher: AccountCipher,
		private readonly readFile: () => Uint8Array | null,
		private readonly writeFile: (bytes: Uint8Array) => void,
	) {}

	save(account: SavedAccount): void {
		const encoded = JSON.stringify(
			Schema.decodeUnknownSync(SavedAccountSchema)(account),
		)
		const sealed = this.cipher.encrypt(encoded)
		this.writeFile(sealed)
	}

	has(): boolean {
		const stored = this.readFile()
		return stored !== null && stored.byteLength > 0
	}

	read(): SavedAccount | null {
		const stored = this.readFile()
		if (!stored || stored.byteLength === 0) {
			return null
		}
		const decoded = Schema.decodeUnknownSync(SavedAccountSchema)(
			JSON.parse(this.cipher.decrypt(stored)),
		)
		return decoded
	}
}
