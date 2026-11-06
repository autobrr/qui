/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import path from 'node:path'
import {
	DEFAULT_QBIT_PORT,
	DEFAULT_QUI_PORT,
	QBIT_CONTAINER_NAME,
	QUI_CONTAINER_NAME,
} from '../shared/constants.js'

export interface DockerDataDirs {
	quiConfig: string
	qbitConfig: string
	downloads: string
}

/** Host directories that survive `docker rm`. Qui keeps its account database here. */
export function dockerDataDirs(home: string, platform: string): DockerDataDirs {
	const root =
		platform === 'darwin'
			? path.join(home, 'Library', 'Application Support', 'Qui', 'docker')
			: platform === 'win32'
				? path.join(home, 'AppData', 'Roaming', 'Qui', 'docker')
				: path.join(home, '.local', 'share', 'qui-desktop', 'docker')
	return {
		quiConfig: path.join(root, 'qui'),
		qbitConfig: path.join(root, 'qbittorrent'),
		downloads: path.join(root, 'downloads'),
	}
}

export function quiContainerRunArgs(
	image: string,
	hostConfig: string,
): string[] {
	return [
		'run',
		'-d',
		'--name',
		QUI_CONTAINER_NAME,
		'-p',
		`${DEFAULT_QUI_PORT}:7476`,
		'-v',
		`${hostConfig}:/config`,
		'--restart',
		'unless-stopped',
		image,
	]
}

export function qbitContainerRunArgs(
	image: string,
	hostConfig: string,
	downloads: string,
): string[] {
	return [
		'run',
		'-d',
		'--name',
		QBIT_CONTAINER_NAME,
		'-p',
		`${DEFAULT_QBIT_PORT}:8080`,
		'-p',
		'6881:6881',
		'-p',
		'6881:6881/udp',
		'-v',
		`${hostConfig}:/config`,
		'-v',
		`${downloads}:/downloads`,
		'--restart',
		'unless-stopped',
		image,
	]
}

/** True when the remote manifest shares any digest with the local image. */
export function imageDigestHasUpdate(
	localText: string,
	remoteText: string,
): boolean {
	const local = digestsIn(localText)
	const remote = digestsIn(remoteText)
	if (local.size === 0 || remote.size === 0) {
		return false
	}
	for (const digest of remote) {
		if (local.has(digest)) {
			return false
		}
	}
	return true
}

function digestsIn(text: string): Set<string> {
	const found = new Set<string>()
	for (const match of text.matchAll(/sha256:[a-f0-9]{64}/gi)) {
		found.add(match[0].toLowerCase())
	}
	return found
}

export interface ImageUpdateOffer {
	image: string
	hasUpdate: boolean
	remoteDigest: string
}

/** Skip a digest the user already dismissed, and skip images that are current. */
export function updatesWorthPrompting(
	updates: readonly ImageUpdateOffer[],
	dismissed: Readonly<Record<string, string>>,
): ImageUpdateOffer[] {
	return updates.filter(
		update =>
			update.hasUpdate && dismissed[update.image] !== update.remoteDigest,
	)
}
