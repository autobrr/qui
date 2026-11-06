/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Schema } from 'effect'
import { QUI_WEB_ORIGIN } from '../shared/constants.js'
import {
	type TorrentTransferStats,
	TorrentTransferStatsSchema,
} from '../shared/types.js'

const RawQbitTransferInfoSchema = Schema.Struct({
	dl_info_speed: Schema.optional(Schema.Number),
	up_info_speed: Schema.optional(Schema.Number),
})

export class QuiApiClient {
	constructor(private readonly baseUrl = QUI_WEB_ORIGIN) {}

	async getTransferStats(): Promise<TorrentTransferStats> {
		try {
			// Try querying Qui / qBittorrent transfer info
			const res = await fetch(`${this.baseUrl}/api/torrents/transfer-info`, {
				signal: AbortSignal.timeout(3000),
			})
			if (!res.ok) {
				return { dlSpeed: 0, upSpeed: 0, activeCount: 0, totalCount: 0 }
			}
			const data: unknown = await res.json()
			const parsed = Schema.decodeUnknownSync(RawQbitTransferInfoSchema)(data)

			return Schema.decodeUnknownSync(TorrentTransferStatsSchema)({
				dlSpeed: parsed.dl_info_speed ?? 0,
				upSpeed: parsed.up_info_speed ?? 0,
				activeCount: 0,
				totalCount: 0,
			})
		} catch {
			return { dlSpeed: 0, upSpeed: 0, activeCount: 0, totalCount: 0 }
		}
	}

	async pauseAll(): Promise<boolean> {
		try {
			const res = await fetch(`${this.baseUrl}/api/torrents/pause-all`, {
				method: 'POST',
				signal: AbortSignal.timeout(5000),
			})
			return res.ok
		} catch {
			return false
		}
	}

	async resumeAll(): Promise<boolean> {
		try {
			const res = await fetch(`${this.baseUrl}/api/torrents/resume-all`, {
				method: 'POST',
				signal: AbortSignal.timeout(5000),
			})
			return res.ok
		} catch {
			return false
		}
	}
}
