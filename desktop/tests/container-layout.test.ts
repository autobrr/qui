/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import path from 'node:path'
import { describe, expect, it } from 'vitest'
import {
	dockerDataDirs,
	imageDigestHasUpdate,
	qbitContainerRunArgs,
	quiContainerRunArgs,
	updatesWorthPrompting,
} from '../src/main/container-layout.js'

describe('Docker data survives container replacement (BR-desktop-11)', () => {
	it('keeps Qui config and downloads under the desktop data directory', () => {
		const home = path.join('Users', 'ada')
		const dirs = dockerDataDirs(home, 'darwin')
		expect(dirs.quiConfig).toBe(
			path.join(home, 'Library', 'Application Support', 'Qui', 'docker', 'qui'),
		)
		expect(dirs.downloads).toBe(
			path.join(
				home,
				'Library',
				'Application Support',
				'Qui',
				'docker',
				'downloads',
			),
		)
	})

	it('mounts the host config directory into the Qui container', () => {
		const args = quiContainerRunArgs('qui-desktop:local', '/host/qui')
		expect(args).toContain('-v')
		expect(args).toContain('/host/qui:/config')
		expect(args.at(-1)).toBe('qui-desktop:local')
	})

	it('mounts qBittorrent config and the download directory', () => {
		const args = qbitContainerRunArgs('qbit:latest', '/host/qbit', '/host/dl')
		expect(args).toContain('/host/qbit:/config')
		expect(args).toContain('/host/dl:/downloads')
	})
})

describe('Docker image prompt (BR-desktop-13)', () => {
	const local =
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
	const other =
		'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

	it('does not offer an update when the manifest still contains the local digest', () => {
		expect(
			imageDigestHasUpdate(
				`ghcr.io/autobrr/qui@${local}`,
				`{"manifests":[{"digest":"${other}"},{"digest":"${local}"}]}`,
			),
		).toBe(false)
	})

	it('offers an update only when none of the remote digests are local', () => {
		expect(imageDigestHasUpdate(`repo@${local}`, `{"digest":"${other}"}`)).toBe(
			true,
		)
	})

	it('does not prompt again for a digest the user dismissed', () => {
		const update = {
			image: 'qui-desktop:local',
			hasUpdate: true,
			remoteDigest: other,
		}
		expect(updatesWorthPrompting([update], {})).toEqual([update])
		expect(
			updatesWorthPrompting([update], { 'qui-desktop:local': other }),
		).toEqual([])
	})
})
