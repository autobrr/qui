/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { app, BrowserWindow, dialog, ipcMain } from 'electron'
import {
	APP_NAME,
	DEFAULT_QBIT_PORT,
	DEFAULT_QUI_PORT,
	QBIT_CONTAINER_NAME,
	QBIT_IMAGE,
	QUI_CONTAINER_NAME,
	QUI_IMAGE,
	QUI_WEB_LOGIN_URL,
	QUI_WEB_ORIGIN,
} from '../shared/constants.js'
import { QuiApiClient } from './api.js'
import { BiometricAuthService } from './auth.js'
import { DockerManager } from './docker.js'
import { openWrappedQui, waitForQuiHttpReady } from './qui-ready.js'
import { AppTray } from './tray.js'
import { AppReleaseUpdater } from './updater.js'
import { createMainWindow } from './window.js'

let mainWindow: BrowserWindow | null = null
let tray: AppTray | null = null
let statsInterval: NodeJS.Timeout | null = null
let isQuitting = false

const dockerManager = new DockerManager()
const biometricAuth = new BiometricAuthService()
/** Suppress repeat Docker image dialogs in one session (tray can force a recheck). */
let dockerImageUpdatePromptShownThisSession = false
const apiClient = new QuiApiClient()
const appUpdater = new AppReleaseUpdater()

// Single instance lock
const gotTheLock = app.requestSingleInstanceLock()
if (!gotTheLock) {
	app.quit()
} else {
	app.on('second-instance', () => {
		if (mainWindow) {
			if (mainWindow.isMinimized()) mainWindow.restore()
			mainWindow.show()
			mainWindow.focus()
			void loadQuiWebUi()
		}
	})
}

const LOADING_SPINNER_SVG = `<svg width="60" height="60" viewBox="0 0 50 50"><g><g transform="rotate(0 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0s" repeatCount="indefinite"></animate></line></g><g transform="rotate(60 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.3s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.3s" repeatCount="indefinite"></animate></line></g><g transform="rotate(120 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.6s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.6s" repeatCount="indefinite"></animate></line></g><g transform="rotate(180 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.8999999999999999s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.8999999999999999s" repeatCount="indefinite"></animate></line></g><g transform="rotate(240 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="1.2s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="1.2s" repeatCount="indefinite"></animate></line></g><g transform="rotate(300 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="1.5s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="1.5s" repeatCount="indefinite"></animate></line></g><circle cx="25" cy="25" r="8" fill="none" stroke="#faf4ff" stroke-width="2"><animate attributeName="r" values="8;9;8" dur="2s" repeatCount="indefinite"></animate></circle></g></svg>`

function waitingPageUrl(message: string, processInfo?: string): string {
	const safeMessage = message
		.replaceAll('&', '&amp;')
		.replaceAll('<', '&lt;')
		.replaceAll('>', '&gt;')
	const codeBlockHtml = processInfo
		? `<pre class="process-code"><code>${processInfo
				.replaceAll('&', '&amp;')
				.replaceAll('<', '&lt;')
				.replaceAll('>', '&gt;')}</code></pre>`
		: ''

	const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
html,body{margin:0;height:100%;background:transparent;color:#f0f6fc;font-family:-apple-system,BlinkMacSystemFont,system-ui,sans-serif;display:flex;align-items:center;justify-content:center}
.card{background:rgba(22,27,34,.78);-webkit-backdrop-filter:blur(24px);backdrop-filter:blur(24px);border:1px solid rgba(255,255,255,.14);border-radius:20px;padding:32px 36px;text-align:center;max-width:440px;box-shadow:0 20px 40px rgba(0,0,0,.45)}
.spinner{display:flex;align-items:center;justify-content:center;margin-bottom:16px}
h1{font-size:24px;margin:0 0 10px;font-weight:650;letter-spacing:-0.02em}
p{margin:0;color:#c9d1d9;font-size:14px;line-height:1.5}
.process-code{margin:16px 0 0;padding:10px 14px;background:rgba(13,17,23,.85);border:1px solid rgba(255,255,255,.1);border-radius:10px;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:12px;color:#a5d6ff;text-align:left;overflow-x:auto;max-height:120px;white-space:pre-wrap;word-break:break-all}
</style></head><body><div class="card"><div class="spinner">${LOADING_SPINNER_SVG}</div><h1>Qui</h1><p>${safeMessage}</p>${codeBlockHtml}</div></body></html>`
	return `data:text/html;charset=utf-8,${encodeURIComponent(html)}`
}

let openingQui: Promise<void> | null = null

async function loadQuiWebUi(): Promise<void> {
	if (!mainWindow) {
		return
	}
	if (openingQui) {
		return openingQui
	}
	const win = mainWindow
	openingQui = (async () => {
		let result: 'opened' | 'unavailable' | 'failed' = 'failed'
		try {
			result = await openWrappedQui({
				showStatus: async (message, processInfo) => {
					await win.loadURL(waitingPageUrl(message, processInfo))
				},
				ensureContainers: async () => {
					const { qui, qbit } = await dockerManager.ensureContainersRunning()
					if (tray) tray.updateDockerStatus(qui, qbit)
				},
				waitForHttp: () => waitForQuiHttpReady({ baseUrl: QUI_WEB_ORIGIN }),
				openWeb: async () => {
					await win.loadURL(QUI_WEB_LOGIN_URL)
				},
				unavailableMessage: `Nothing is serving ${QUI_WEB_ORIGIN} yet. Check Docker, then use Tray → Reload Qui web app.`,
			})
		} catch (error) {
			const detail =
				error instanceof Error ? error.message : 'Docker or Qui failed to start'
			await win.loadURL(waitingPageUrl('Failed to start Qui', detail))
		} finally {
			openingQui = null
		}
		if (result === 'opened') {
			await checkAndPromptDockerUpdates()
			const appRelease = await appUpdater.checkForUpdates()
			if (appRelease.hasUpdate && mainWindow) {
				await appUpdater.promptUpdateIfAvailable(mainWindow, appRelease)
			}
		}
	})()
	return openingQui
}

function registerIpcHandlers(): void {
	ipcMain.handle('auth:biometrics', async (_event, reason?: string) =>
		biometricAuth.authenticate(
			typeof reason === 'string' && reason.length > 0
				? reason
				: 'Secure your Qui session',
		),
	)

	ipcMain.handle('docker:status', async () => {
		const qui = await dockerManager.getContainerStatus(
			QUI_CONTAINER_NAME,
			DEFAULT_QUI_PORT,
			QUI_IMAGE,
		)
		const qbit = await dockerManager.getContainerStatus(
			QBIT_CONTAINER_NAME,
			DEFAULT_QBIT_PORT,
			QBIT_IMAGE,
		)
		return { qui, qbit }
	})

	ipcMain.handle('docker:check-updates', async () => {
		const quiUpdate = await dockerManager.checkForImageUpdate(QUI_IMAGE)
		const qbitUpdate = await dockerManager.checkForImageUpdate(QBIT_IMAGE)
		return [quiUpdate, qbitUpdate]
	})

	ipcMain.handle(
		'docker:apply-update',
		async (
			_event,
			{
				containerName,
				image,
				port,
			}: { containerName: string; image: string; port: number },
		) => {
			return dockerManager.updateAndRestartContainer(containerName, image, port)
		},
	)

	ipcMain.handle('api:transfer-stats', async () => {
		return apiClient.getTransferStats()
	})

	ipcMain.handle('updater:check-app-update', async () => {
		return appUpdater.checkForUpdates()
	})
}

async function checkAndPromptDockerUpdates(options?: {
	force?: boolean
}): Promise<void> {
	try {
		if (!options?.force && dockerImageUpdatePromptShownThisSession) {
			return
		}
		const quiUpdate = await dockerManager.checkForImageUpdate(QUI_IMAGE)
		const qbitUpdate = await dockerManager.checkForImageUpdate(QBIT_IMAGE)

		const updates = [quiUpdate, qbitUpdate].filter(u => u.hasUpdate)
		if (updates.length > 0 && mainWindow) {
			dockerImageUpdatePromptShownThisSession = true
			const names = updates.map(u => u.image).join(', ')
			const { response } = await dialog.showMessageBox(mainWindow, {
				type: 'question',
				title: `${APP_NAME} - Docker Image Updates Available`,
				message: `Updates are available for: ${names}`,
				detail:
					'Would you like to pull the latest Docker image(s) and restart the containers now?',
				buttons: ['Update Now', 'Later'],
				defaultId: 0,
				cancelId: 1,
			})

			if (response === 0) {
				for (const u of updates) {
					const isQui = u.image === QUI_IMAGE
					const name = isQui ? QUI_CONTAINER_NAME : QBIT_CONTAINER_NAME
					const port = isQui ? DEFAULT_QUI_PORT : DEFAULT_QBIT_PORT
					await dockerManager.updateAndRestartContainer(name, u.image, port)
				}
				dialog.showMessageBox(mainWindow, {
					type: 'info',
					title: APP_NAME,
					message: 'Containers updated successfully!',
				})
				void loadQuiWebUi()
			}
		}
	} catch {
		// Non-blocking update check
	}
}

async function bootstrapApp(): Promise<void> {
	registerIpcHandlers()
	mainWindow = createMainWindow()

	tray = new AppTray(mainWindow, {
		onShowApp: () => {
			if (mainWindow) {
				mainWindow.show()
				mainWindow.focus()
			}
		},
		onReloadQui: () => {
			void loadQuiWebUi()
		},
		onPauseAll: async () => {
			await apiClient.pauseAll()
		},
		onResumeAll: async () => {
			await apiClient.resumeAll()
		},
		onAddTorrent: () => {
			if (mainWindow) {
				mainWindow.show()
				mainWindow.focus()
			}
		},
		onCheckUpdates: async () => {
			await checkAndPromptDockerUpdates({ force: true })
		},
		onCheckAppUpdates: async () => {
			const info = await appUpdater.checkForUpdates()
			await appUpdater.promptUpdateIfAvailable(mainWindow, info)
		},
		onQuitApp: () => {
			app.quit()
		},
	})
	tray.init()

	void loadQuiWebUi()

	statsInterval = setInterval(async () => {
		const stats = await apiClient.getTransferStats()
		if (tray) tray.updateStats(stats)
	}, 2000)

	mainWindow.on('close', event => {
		if (!isQuitting) {
			event.preventDefault()
			mainWindow?.hide()
		}
	})
}

app.on('before-quit', () => {
	isQuitting = true
	if (statsInterval) {
		clearInterval(statsInterval)
		statsInterval = null
	}
	if (tray) {
		tray.destroy()
		tray = null
	}
})

app.whenReady().then(bootstrapApp)

app.on('window-all-closed', () => {
	if (process.platform !== 'darwin') {
		app.quit()
	}
})

app.on('activate', () => {
	if (BrowserWindow.getAllWindows().length === 0) {
		void bootstrapApp()
	} else if (mainWindow) {
		mainWindow.show()
	}
})
