/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import path from 'node:path'
import { describe, expect, it } from 'vitest'
import {
	DOCKER_BINARY_NOT_FOUND,
	type DockerExecutor,
	DockerManager,
	dockerBinaryCandidates,
	resolveDockerBinary,
} from '../src/main/docker.js'
import {
	DEFAULT_QUI_PORT,
	QUI_CONTAINER_NAME,
	QUI_IMAGE,
} from '../src/shared/constants.js'

class MockDockerExecutor implements DockerExecutor {
	public executedCommands: Array<{ command: string; args: string[] }> = []
	public responses: Record<string, { stdout: string; stderr: string }> = {}

	async exec(
		command: string,
		args: string[],
	): Promise<{ stdout: string; stderr: string }> {
		this.executedCommands.push({ command, args })
		const key = `${command} ${args.join(' ')}`
		if (this.responses[key]) {
			return this.responses[key]
		}
		// Prefix match
		for (const [k, v] of Object.entries(this.responses)) {
			if (key.startsWith(k)) {
				return v
			}
		}
		return { stdout: '', stderr: '' }
	}
}

describe('Docker CLI resolution (BR-desktop-10)', () => {
	it('checks the Docker Desktop user CLI, Homebrew, /usr/local, then the app bundle', () => {
		const home = path.join('Users', 'example')
		expect(dockerBinaryCandidates(home)).toEqual([
			path.join(home, '.docker', 'bin', 'docker'),
			'/opt/homebrew/bin/docker',
			'/usr/local/bin/docker',
			'/Applications/Docker.app/Contents/Resources/bin/docker',
		])
	})

	it('uses the first candidate that exists', () => {
		const home = path.join('Users', 'example')
		const homebrew = '/opt/homebrew/bin/docker'
		const resolved = resolveDockerBinary(
			candidate => candidate === homebrew,
			home,
		)
		expect(resolved).toBe(homebrew)
	})

	it('reports docker binary not found when every candidate is missing', () => {
		expect(() =>
			resolveDockerBinary(() => false, path.join('Users', 'example')),
		).toThrow(DOCKER_BINARY_NOT_FOUND)
	})
})

describe('DockerManager', () => {
	it('checks if docker is available when docker info succeeds', async () => {
		const mock = new MockDockerExecutor()
		mock.responses['docker info --format {{.ServerVersion}}'] = {
			stdout: '24.0.7\n',
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const available = await manager.isDockerAvailable()
		expect(available).toBe(true)
	})

	it('returns false when docker info fails', async () => {
		const mock: DockerExecutor = {
			async exec() {
				throw new Error('Docker daemon not running')
			},
		}

		const manager = new DockerManager(mock)
		const available = await manager.isDockerAvailable()
		expect(available).toBe(false)
	})

	it('inspects container status correctly when running', async () => {
		const mock = new MockDockerExecutor()
		mock.responses[
			'docker inspect --format {{.State.Status}}|{{.State.Running}} qui'
		] = {
			stdout: 'running|true\n',
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const status = await manager.getContainerStatus(
			QUI_CONTAINER_NAME,
			DEFAULT_QUI_PORT,
			QUI_IMAGE,
		)

		expect(status.name).toBe('qui')
		expect(status.state).toBe('running')
		expect(status.status).toBe('running')
		expect(status.port).toBe(7476)
	})

	it('returns not_found when container inspect fails', async () => {
		const mock: DockerExecutor = {
			async exec() {
				throw new Error('No such container')
			},
		}

		const manager = new DockerManager(mock)
		const status = await manager.getContainerStatus(
			QUI_CONTAINER_NAME,
			DEFAULT_QUI_PORT,
			QUI_IMAGE,
		)

		expect(status.state).toBe('not_found')
		expect(status.status).toBe('not_installed')
	})

	it('starts stopped containers during ensureContainersRunning', async () => {
		const mock = new MockDockerExecutor()
		mock.responses[
			'docker inspect --format {{.State.Status}}|{{.State.Running}} qui'
		] = {
			stdout: 'exited|false\n',
			stderr: '',
		}
		mock.responses[
			'docker inspect --format {{.State.Status}}|{{.State.Running}} qbittorrent-nox'
		] = {
			stdout: 'running|true\n',
			stderr: '',
		}
		mock.responses['docker start qui'] = { stdout: 'qui\n', stderr: '' }

		const manager = new DockerManager(mock)
		await manager.ensureContainersRunning()

		expect(
			mock.executedCommands.some(
				c =>
					c.command === 'docker' &&
					c.args[0] === 'start' &&
					c.args[1] === 'qui',
			),
		).toBe(true)
	})

	it('detects available updates when image digests differ', async () => {
		const mock = new MockDockerExecutor()
		mock.responses[
			`docker inspect --format {{range .RepoDigests}}{{.}}{{end}} ${QUI_IMAGE}`
		] = {
			stdout:
				'ghcr.io/autobrr/qui@sha256:1111111111111111111111111111111111111111111111111111111111111111',
			stderr: '',
		}
		mock.responses[`docker buildx imagetools inspect --raw ${QUI_IMAGE}`] = {
			stdout:
				'{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","digest":"sha256:2222222222222222222222222222222222222222222222222222222222222222"}',
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const update = await manager.checkForImageUpdate(QUI_IMAGE)

		expect(update.hasUpdate).toBe(true)
		expect(update.image).toBe(QUI_IMAGE)
	})

	it('detects no updates when local repo digest line lists multiple digests including the remote', async () => {
		const remote =
			'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
		const other =
			'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
		const mock = new MockDockerExecutor()
		mock.responses[
			`docker inspect --format {{range .RepoDigests}}{{.}}{{end}} ${QUI_IMAGE}`
		] = {
			stdout:
				`ghcr.io/autobrr/qui@${remote}\n` + `ghcr.io/autobrr/qui@${other}\n`,
			stderr: '',
		}
		mock.responses[`docker buildx imagetools inspect --raw ${QUI_IMAGE}`] = {
			stdout: `{"digest":"${remote}"}`,
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const update = await manager.checkForImageUpdate(QUI_IMAGE)

		expect(update.hasUpdate).toBe(false)
	})

	it('does not report an update when the manifest list still contains the local platform digest', async () => {
		const local =
			'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
		const index =
			'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'
		const mock = new MockDockerExecutor()
		mock.responses[
			`docker inspect --format {{range .RepoDigests}}{{.}}{{end}} ${QUI_IMAGE}`
		] = {
			stdout: `qui-desktop@${local}`,
			stderr: '',
		}
		mock.responses[`docker buildx imagetools inspect --raw ${QUI_IMAGE}`] = {
			stdout: `{"manifests":[{"digest":"${index}"},{"digest":"${local}"}]}`,
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const update = await manager.checkForImageUpdate(QUI_IMAGE)
		expect(update.hasUpdate).toBe(false)
	})

	it('detects no updates when image digests match', async () => {
		const digest =
			'sha256:3333333333333333333333333333333333333333333333333333333333333333'
		const mock = new MockDockerExecutor()
		mock.responses[
			`docker inspect --format {{range .RepoDigests}}{{.}}{{end}} ${QUI_IMAGE}`
		] = {
			stdout: `ghcr.io/autobrr/qui@${digest}`,
			stderr: '',
		}
		mock.responses[`docker buildx imagetools inspect --raw ${QUI_IMAGE}`] = {
			stdout: `{"digest":"${digest}"}`,
			stderr: '',
		}

		const manager = new DockerManager(mock)
		const update = await manager.checkForImageUpdate(QUI_IMAGE)

		expect(update.hasUpdate).toBe(false)
	})

	it('copies config before removing a container that has no host mount', async () => {
		const mock = new MockDockerExecutor()
		mock.responses[
			'docker inspect --format {{.Config.Image}}||{{range .Mounts}}{{.Destination}}={{.Source}};{{end}} qui'
		] = {
			stdout: 'ghcr.io/autobrr/qui:latest||\n',
			stderr: '',
		}
		const created: string[] = []
		const manager = new DockerManager(mock, dir => {
			created.push(dir)
		})

		await manager.alignContainer('qui', QUI_IMAGE, '/host/qui', '/config')

		expect(created).toEqual(['/host/qui'])
		expect(mock.executedCommands.map(c => c.args[0])).toEqual([
			'inspect',
			'stop',
			'cp',
			'rm',
		])
	})

	it('leaves the container alone when the copy fails', async () => {
		const mock = new MockDockerExecutor()
		mock.responses[
			'docker inspect --format {{.Config.Image}}||{{range .Mounts}}{{.Destination}}={{.Source}};{{end}} qui'
		] = {
			stdout: 'old:image||\n',
			stderr: '',
		}
		const original = mock.exec.bind(mock)
		mock.exec = async (command, args) => {
			if (args[0] === 'cp') {
				throw new Error('copy failed')
			}
			return original(command, args)
		}
		const manager = new DockerManager(mock, () => {})

		await expect(
			manager.alignContainer('qui', QUI_IMAGE, '/host/qui', '/config'),
		).rejects.toThrow('copy failed')
		expect(mock.executedCommands.map(c => c.args[0])).toEqual([
			'inspect',
			'stop',
		])
		expect(mock.executedCommands.some(c => c.args[0] === 'rm')).toBe(false)
	})

	it('updates and restarts container cleanly', async () => {
		const mock = new MockDockerExecutor()
		const manager = new DockerManager(mock)

		const success = await manager.updateAndRestartContainer(
			QUI_CONTAINER_NAME,
			QUI_IMAGE,
		)

		expect(success).toBe(true)
		expect(mock.executedCommands.map(c => c.args[0])).toEqual([
			'pull',
			'stop',
			'rm',
			'run',
		])
	})
})
