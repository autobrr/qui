/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import path from 'node:path'
import { Schema } from 'effect'
import {
	app,
	BrowserWindow,
	dialog,
	ipcMain,
	nativeTheme,
	safeStorage,
} from 'electron'
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
import { AccountVault } from './account-vault.js'
import { QuiApiClient } from './api.js'
import { BiometricAuthService } from './auth.js'
import { dockerDataDirs, updatesWorthPrompting } from './container-layout.js'
import { DockerManager } from './docker.js'
import { openWrappedQui, waitForQuiHttpReady } from './qui-ready.js'
import { startupPageUrl } from './startup-page.js'
import { AppTray } from './tray.js'
import { AppReleaseUpdater } from './updater.js'
import {
	applyTrafficLights,
	applyWindowMaterial,
	createMainWindow,
} from './window.js'

const DismissedDigestsSchema = Schema.Record({
	key: Schema.String,
	value: Schema.String,
})

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

function showStartupPage(message: string, processInfo?: string): string {
	return startupPageUrl(
		message,
		processInfo && processInfo.length > 0 ? processInfo : message,
		nativeTheme.shouldUseDarkColors,
	)
}

async function presentStartupPage(
	win: BrowserWindow,
	message: string,
	processInfo?: string,
): Promise<void> {
	await win.loadURL(showStartupPage(message, processInfo))
	if (!win.isVisible()) {
		win.show()
	}
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
					await presentStartupPage(win, message, processInfo)
				},
				ensureContainers: async () => {
					const dirs = dockerDataDirs(homedir(), process.platform)
					for (const dir of [dirs.quiConfig, dirs.qbitConfig, dirs.downloads]) {
						mkdirSync(dir, { recursive: true })
					}
					await dockerManager.alignContainer(
						QUI_CONTAINER_NAME,
						QUI_IMAGE,
						dirs.quiConfig,
						'/config',
					)
					await dockerManager.alignContainer(
						QBIT_CONTAINER_NAME,
						QBIT_IMAGE,
						dirs.qbitConfig,
						'/config',
					)
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
			await presentStartupPage(win, 'Failed to start Qui', detail)
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
			{ containerName, image }: { containerName: string; image: string },
		) => {
			return dockerManager.updateAndRestartContainer(containerName, image)
		},
	)

	ipcMain.handle('api:transfer-stats', async () => {
		return apiClient.getTransferStats()
	})

	ipcMain.handle('updater:check-app-update', async () => {
		return appUpdater.checkForUpdates()
	})

	ipcMain.handle('window:set-material', async (_event, glass: unknown) => {
		if (!mainWindow || typeof glass !== 'boolean') {
			return
		}
		applyWindowMaterial(mainWindow, glass)
	})

	ipcMain.handle('window:set-sidebar', async (_event, visible: unknown) => {
		if (!mainWindow || typeof visible !== 'boolean') {
			return
		}
		applyTrafficLights(mainWindow, visible)
	})

	ipcMain.handle(
		'account:save',
		async (_event, username: unknown, password: unknown) => {
			if (
				typeof username !== 'string' ||
				typeof password !== 'string' ||
				!safeStorage.isEncryptionAvailable()
			) {
				return
			}
			accountVault().save({ username, password })
		},
	)

	ipcMain.handle('account:has', async () => accountVault().has())

	ipcMain.handle('account:unlock', async () => {
		const auth = await biometricAuth.authenticate('Sign in to Qui')
		if (!auth.success) {
			return null
		}
		return accountVault().read()
	})
}

function dismissalFile(): string {
	return path.join(app.getPath('userData'), 'dismissed-docker-digests.json')
}

function readDismissedDigests(): Record<string, string> {
	try {
		return Schema.decodeUnknownSync(DismissedDigestsSchema)(
			JSON.parse(readFileSync(dismissalFile(), 'utf8')),
		)
	} catch {
		return {}
	}
}

function writeDismissedDigests(value: Record<string, string>): void {
	writeFileSync(dismissalFile(), JSON.stringify(value))
}

function accountVault(): AccountVault {
	const file = path.join(app.getPath('userData'), 'account.bin')
	return new AccountVault(
		{
			encrypt: plain => safeStorage.encryptString(plain),
			decrypt: bytes => safeStorage.decryptString(Buffer.from(bytes)),
		},
		() => {
			try {
				return readFileSync(file)
			} catch {
				return null
			}
		},
		bytes => {
			writeFileSync(file, Buffer.from(bytes))
		},
	)
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

		const updates = updatesWorthPrompting(
			[quiUpdate, qbitUpdate],
			options?.force ? {} : readDismissedDigests(),
		)
		if (updates.length > 0 && mainWindow) {
			dockerImageUpdatePromptShownThisSession = true
			const names = updates.map(u => u.image).join(', ')
			const { response } = await dialog.showMessageBox(mainWindow, {
				type: 'question',
				title: `${APP_NAME} - Docker Image Updates Available`,
				message: `Updates are available for: ${names}`,
				detail:
					'Would you like to pull the latest Docker image(s) and restart the containers now? Your Qui account stays in the desktop data folder.',
				buttons: ['Update Now', 'Later'],
				defaultId: 1,
				cancelId: 1,
			})

			if (response !== 0) {
				const dismissed = readDismissedDigests()
				for (const update of updates) {
					dismissed[update.image] = update.remoteDigest
				}
				writeDismissedDigests(dismissed)
				return
			}

			if (response === 0) {
				for (const u of updates) {
					const name =
						u.image === QUI_IMAGE ? QUI_CONTAINER_NAME : QBIT_CONTAINER_NAME
					await dockerManager.updateAndRestartContainer(name, u.image)
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
