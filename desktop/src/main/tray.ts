/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type BrowserWindow, Menu, nativeImage, Tray } from 'electron'
import { APP_NAME } from '../shared/constants.js'
import type {
	DockerContainerStatus,
	TorrentTransferStats,
} from '../shared/types.js'

export function formatSpeed(bytesPerSec: number): string {
	if (bytesPerSec <= 0) return '0 B/s'
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
	const index = Math.min(
		Math.floor(Math.log(bytesPerSec) / Math.log(1024)),
		units.length - 1,
	)
	const val = bytesPerSec / 1024 ** index
	return `${val.toFixed(val >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
}

export function formatTrayTitle(stats?: TorrentTransferStats): string {
	if (!stats || (stats.dlSpeed === 0 && stats.upSpeed === 0)) {
		return APP_NAME
	}
	return `↓ ${formatSpeed(stats.dlSpeed)} ↑ ${formatSpeed(stats.upSpeed)}`
}

export interface TrayCallbacks {
	onShowApp: () => void
	/** Reload the remote Qui web UI (same as browser tab refresh). */
	onReloadQui: () => void
	onPauseAll: () => Promise<void>
	onResumeAll: () => Promise<void>
	onAddTorrent: () => void
	onCheckUpdates: () => Promise<void>
	onCheckAppUpdates?: () => Promise<void>
	onQuitApp: () => void
}

export class AppTray {
	private tray: Tray | null = null
	private currentStats: TorrentTransferStats = {
		dlSpeed: 0,
		upSpeed: 0,
		activeCount: 0,
		totalCount: 0,
	}
	private quiStatus: DockerContainerStatus = {
		name: 'qui',
		state: 'unknown',
		status: 'checking',
		image: 'ghcr.io/autobrr/qui:latest',
		port: 7476,
	}
	private qbitStatus: DockerContainerStatus = {
		name: 'qbittorrent-nox',
		state: 'unknown',
		status: 'checking',
		image: 'lscr.io/linuxserver/qbittorrent:latest',
		port: 8080,
	}

	constructor(
		readonly _window: BrowserWindow | null,
		private readonly callbacks: TrayCallbacks,
	) {}

	init(): void {
		// Create a 16x16 monochromatic template icon for macOS status bar
		const icon = nativeImage.createFromBuffer(
			Buffer.from(
				'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAAXNSR0IArs4c6QAAAFNJREFUOE9jZKAQMFKon2H4f+p/RiIBI24FjDAGw6f///+ToxmjChB4+p8B2Q1/Gf6TYgC2w4E0j8A0o3sC1YKhZAZuG7AbjqwRzYJhY8BwNQMAt24v7dG7jWIAAAAASUVORK5CYII=',
				'base64',
			),
		)
		icon.setTemplateImage(true)

		this.tray = new Tray(icon)
		this.tray.setToolTip(`${APP_NAME} - qBittorrent WebUI`)
		this.tray.setTitle(APP_NAME)

		this.tray.on('click', () => {
			this.callbacks.onShowApp()
		})

		this.updateMenu()
	}

	updateStats(stats: TorrentTransferStats): void {
		this.currentStats = stats
		if (this.tray) {
			this.tray.setTitle(formatTrayTitle(stats))
			this.updateMenu()
		}
	}

	updateDockerStatus(
		qui: DockerContainerStatus,
		qbit: DockerContainerStatus,
	): void {
		this.quiStatus = qui
		this.qbitStatus = qbit
		this.updateMenu()
	}

	private updateMenu(): void {
		if (!this.tray) return

		const contextMenu = Menu.buildFromTemplate([
			{
				label: `${APP_NAME} Desktop`,
				enabled: false,
			},
			{ type: 'separator' },
			{
				label: `↓ ${formatSpeed(this.currentStats.dlSpeed)}   ↑ ${formatSpeed(this.currentStats.upSpeed)}`,
				enabled: false,
			},
			{
				label: `Active: ${this.currentStats.activeCount}   Total: ${this.currentStats.totalCount}`,
				enabled: false,
			},
			{ type: 'separator' },
			{
				label: 'Pause All Torrents',
				click: () => {
					void this.callbacks.onPauseAll()
				},
			},
			{
				label: 'Resume All Torrents',
				click: () => {
					void this.callbacks.onResumeAll()
				},
			},
			{
				label: 'Add Torrent...',
				click: () => {
					this.callbacks.onAddTorrent()
				},
			},
			{ type: 'separator' },
			{
				label: 'Docker Containers',
				submenu: [
					{
						label: `Qui Service: ${this.quiStatus.state} (${this.quiStatus.status})`,
						enabled: false,
					},
					{
						label: `qBittorrent Daemon: ${this.qbitStatus.state} (${this.qbitStatus.status})`,
						enabled: false,
					},
					{ type: 'separator' },
					{
						label: 'Check for Docker Updates...',
						click: () => {
							void this.callbacks.onCheckUpdates()
						},
					},
				],
			},
			{ type: 'separator' },
			{
				label: `Check for ${APP_NAME} Updates...`,
				click: () => {
					void this.callbacks.onCheckAppUpdates?.()
				},
			},
			{
				label: `Show ${APP_NAME}`,
				click: () => {
					this.callbacks.onShowApp()
				},
			},
			{
				label: 'Reload Qui web app',
				click: () => {
					this.callbacks.onReloadQui()
				},
			},
			{ type: 'separator' },
			{
				label: `Quit ${APP_NAME}`,
				click: () => {
					this.callbacks.onQuitApp()
				},
			},
		])

		this.tray.setContextMenu(contextMenu)
	}

	destroy(): void {
		if (this.tray) {
			this.tray.destroy()
			this.tray = null
		}
	}
}
