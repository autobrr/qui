/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Schema } from 'effect'
import { type BrowserWindow, dialog, shell } from 'electron'
import {
	APP_NAME,
	CURRENT_APP_VERSION,
	GITHUB_REPO,
} from '../shared/constants.js'
import { type AppReleaseInfo, AppReleaseInfoSchema } from '../shared/types.js'

export function parseSemver(v: string): [number, number, number] {
	const cleaned = v.trim().replace(/^v/i, '')
	const parts = cleaned.split('.').map(p => Number.parseInt(p, 10))
	return [parts[0] || 0, parts[1] || 0, parts[2] || 0]
}

export function compareSemver(v1: string, v2: string): number {
	const [maj1, min1, pat1] = parseSemver(v1)
	const [maj2, min2, pat2] = parseSemver(v2)

	if (maj1 !== maj2) return maj1 - maj2
	if (min1 !== min2) return min1 - min2
	return pat1 - pat2
}

export interface ReleaseFetcher {
	fetchLatest(repo: string): Promise<unknown>
}

export class GitHubReleaseFetcher implements ReleaseFetcher {
	async fetchLatest(repo: string): Promise<unknown> {
		const res = await fetch(
			`https://api.github.com/repos/${repo}/releases/latest`,
			{
				headers: {
					'User-Agent': `${APP_NAME}-Desktop-Updater`,
					Accept: 'application/vnd.github.v3+json',
				},
				signal: AbortSignal.timeout(8000),
			},
		)
		if (!res.ok) {
			throw new Error(`Failed to query releases: HTTP ${res.status}`)
		}
		return res.json()
	}
}

export class AppReleaseUpdater {
	constructor(
		private readonly repo: string = GITHUB_REPO,
		private readonly currentVersion: string = CURRENT_APP_VERSION,
		private readonly fetcher: ReleaseFetcher = new GitHubReleaseFetcher(),
	) {}

	async checkForUpdates(): Promise<AppReleaseInfo> {
		try {
			const data = (await this.fetcher.fetchLatest(this.repo)) as Record<
				string,
				unknown
			>
			const tagName = String(data.tag_name ?? '')
			const latestVersion = tagName.replace(/^v/i, '') || this.currentVersion
			const hasUpdate = compareSemver(latestVersion, this.currentVersion) > 0

			let downloadUrl = ''
			if (Array.isArray(data.assets) && data.assets.length > 0) {
				const macAsset = (
					data.assets as Array<{ browser_download_url?: string; name?: string }>
				).find(a => a.name?.endsWith('.dmg') || a.name?.endsWith('.zip'))
				downloadUrl =
					macAsset?.browser_download_url ?? String(data.html_url ?? '')
			}

			return Schema.decodeUnknownSync(AppReleaseInfoSchema)({
				hasUpdate,
				currentVersion: this.currentVersion,
				latestVersion,
				tagName,
				name: String(data.name ?? `Release ${tagName}`),
				body: String(data.body ?? ''),
				htmlUrl: String(
					data.html_url ?? `https://github.com/${this.repo}/releases`,
				),
				downloadUrl: downloadUrl || undefined,
				publishedAt: String(data.published_at ?? new Date().toISOString()),
			})
		} catch {
			return Schema.decodeUnknownSync(AppReleaseInfoSchema)({
				hasUpdate: false,
				currentVersion: this.currentVersion,
				latestVersion: this.currentVersion,
				tagName: `v${this.currentVersion}`,
				name: `${APP_NAME} v${this.currentVersion}`,
				body: '',
				htmlUrl: `https://github.com/${this.repo}/releases`,
				publishedAt: new Date().toISOString(),
			})
		}
	}

	async promptUpdateIfAvailable(
		window: BrowserWindow | null,
		info: AppReleaseInfo,
	): Promise<boolean> {
		if (!info.hasUpdate) {
			if (window) {
				await dialog.showMessageBox(window, {
					type: 'info',
					title: APP_NAME,
					message: `You are using the latest version of ${APP_NAME} (v${this.currentVersion}).`,
				})
			}
			return false
		}

		if (!window) return false

		const { response } = await dialog.showMessageBox(window, {
			type: 'info',
			title: `${APP_NAME} - New Version Available`,
			message: `A new version of ${APP_NAME} is available: v${info.latestVersion}`,
			detail: `Current version: v${this.currentVersion}\n\n${info.body ? `Release notes:\n${info.body.slice(0, 300)}...` : ''}`,
			buttons: ['Download Update', 'Later'],
			defaultId: 0,
			cancelId: 1,
		})

		if (response === 0) {
			const targetUrl = info.downloadUrl || info.htmlUrl
			await shell.openExternal(targetUrl)
			return true
		}

		return false
	}
}
