/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { Schema } from 'effect'
import {
	DEFAULT_QBIT_PORT,
	DEFAULT_QUI_PORT,
	QBIT_CONTAINER_NAME,
	QBIT_IMAGE,
	QUI_CONTAINER_NAME,
	QUI_IMAGE,
} from '../shared/constants.js'
import {
	type DockerContainerStatus,
	DockerContainerStatusSchema,
	type DockerUpdateInfo,
	DockerUpdateInfoSchema,
} from '../shared/types.js'

const execFileAsync = promisify(execFile)

export interface DockerExecutor {
	exec(
		command: string,
		args: string[],
	): Promise<{ stdout: string; stderr: string }>
}

/** Standard PATH additions for macOS GUI apps (which don't inherit interactive shell PATH). */
const DEFAULT_EXEC_PATH = [
	'/opt/homebrew/bin',
	'/usr/local/bin',
	'/usr/bin',
	'/bin',
	'/usr/sbin',
	'/sbin',
	process.env.PATH ?? '',
]
	.filter(Boolean)
	.join(':')

export class CliDockerExecutor implements DockerExecutor {
	async exec(
		command: string,
		args: string[],
	): Promise<{ stdout: string; stderr: string }> {
		return execFileAsync(command, args, {
			timeout: 120000,
			env: {
				...process.env,
				PATH: DEFAULT_EXEC_PATH,
			},
		})
	}
}

export class DockerManager {
	constructor(
		private readonly executor: DockerExecutor = new CliDockerExecutor(),
	) {}

	async isDockerAvailable(): Promise<boolean> {
		try {
			const { stdout } = await this.executor.exec('docker', [
				'info',
				'--format',
				'{{.ServerVersion}}',
			])
			return stdout.trim().length > 0
		} catch {
			return false
		}
	}

	async getContainerStatus(
		containerName: string,
		port: number,
		image: string,
	): Promise<DockerContainerStatus> {
		try {
			const { stdout } = await this.executor.exec('docker', [
				'inspect',
				'--format',
				'{{.State.Status}}|{{.State.Running}}',
				containerName,
			])
			const [status, running] = stdout.trim().split('|')
			const state = running === 'true' ? 'running' : 'stopped'

			return Schema.decodeUnknownSync(DockerContainerStatusSchema)({
				name: containerName,
				state,
				status: status ?? 'unknown',
				image,
				port,
			})
		} catch {
			return Schema.decodeUnknownSync(DockerContainerStatusSchema)({
				name: containerName,
				state: 'not_found',
				status: 'not_installed',
				image,
				port,
			})
		}
	}

	async ensureContainersRunning(): Promise<{
		qui: DockerContainerStatus
		qbit: DockerContainerStatus
	}> {
		const quiStatus = await this.ensureContainer(
			QUI_CONTAINER_NAME,
			QUI_IMAGE,
			DEFAULT_QUI_PORT,
			[
				'run',
				'-d',
				'--name',
				QUI_CONTAINER_NAME,
				'-p',
				`${DEFAULT_QUI_PORT}:7476`,
				'--restart',
				'unless-stopped',
				QUI_IMAGE,
			],
		)

		const qbitStatus = await this.ensureContainer(
			QBIT_CONTAINER_NAME,
			QBIT_IMAGE,
			DEFAULT_QBIT_PORT,
			[
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
				'--restart',
				'unless-stopped',
				QBIT_IMAGE,
			],
		)

		return { qui: quiStatus, qbit: qbitStatus }
	}

	private async ensureContainer(
		name: string,
		image: string,
		port: number,
		runArgs: string[],
	): Promise<DockerContainerStatus> {
		const current = await this.getContainerStatus(name, port, image)

		if (current.state === 'running') {
			return current
		}

		if (current.state === 'stopped') {
			await this.executor.exec('docker', ['start', name])
			return this.getContainerStatus(name, port, image)
		}

		// Not found: pull and run
		await this.executor.exec('docker', ['pull', image])
		await this.executor.exec('docker', runArgs)
		return this.getContainerStatus(name, port, image)
	}

	async checkForImageUpdate(imageName: string): Promise<DockerUpdateInfo> {
		try {
			// Get local image repo digest
			const { stdout: localDigestOut } = await this.executor.exec('docker', [
				'inspect',
				'--format',
				'{{range .RepoDigests}}{{.}}{{end}}',
				imageName,
			])
			const localDigest = localDigestOut.trim()

			// Inspect remote image manifest using docker manifest inspect / buildx imagetools inspect
			let remoteDigest = ''
			try {
				const { stdout: remoteOut } = await this.executor.exec('docker', [
					'buildx',
					'imagetools',
					'inspect',
					'--raw',
					imageName,
				])
				// Parse manifest digest or sha if available
				const hashMatch = remoteOut.match(/sha256:[a-f0-9]{64}/i)
				remoteDigest = hashMatch ? hashMatch[0] : ''
			} catch {
				// Fallback to docker manifest inspect
				const { stdout: manifestOut } = await this.executor.exec('docker', [
					'manifest',
					'inspect',
					imageName,
				])
				const hashMatch = manifestOut.match(/sha256:[a-f0-9]{64}/i)
				remoteDigest = hashMatch ? hashMatch[0] : ''
			}

			const extractDigests = (s: string): Set<string> => {
				const found = new Set<string>()
				for (const m of s.matchAll(/sha256:[a-f0-9]{64}/gi)) {
					found.add(m[0].toLowerCase())
				}
				return found
			}
			const localSet = extractDigests(localDigest)
			const remoteNorm = remoteDigest.toLowerCase()
			const hasUpdate = Boolean(
				localSet.size > 0 && remoteNorm.length > 0 && !localSet.has(remoteNorm),
			)

			return Schema.decodeUnknownSync(DockerUpdateInfoSchema)({
				hasUpdate,
				image: imageName,
				localDigest: localDigest || 'unknown',
				remoteDigest: remoteDigest || 'unknown',
			})
		} catch {
			return Schema.decodeUnknownSync(DockerUpdateInfoSchema)({
				hasUpdate: false,
				image: imageName,
				localDigest: 'unknown',
				remoteDigest: 'unknown',
			})
		}
	}

	async updateAndRestartContainer(
		containerName: string,
		image: string,
		port: number,
	): Promise<boolean> {
		try {
			await this.executor.exec('docker', ['pull', image])
			await this.executor.exec('docker', ['stop', containerName])
			await this.executor.exec('docker', ['rm', containerName])

			if (containerName === QUI_CONTAINER_NAME) {
				await this.executor.exec('docker', [
					'run',
					'-d',
					'--name',
					containerName,
					'-p',
					`${port}:7476`,
					'--restart',
					'unless-stopped',
					image,
				])
			} else {
				await this.executor.exec('docker', [
					'run',
					'-d',
					'--name',
					containerName,
					'-p',
					`${port}:8080`,
					'-p',
					'6881:6881',
					'-p',
					'6881:6881/udp',
					'--restart',
					'unless-stopped',
					image,
				])
			}
			return true
		} catch {
			return false
		}
	}
}
