/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Schema } from 'effect'

export const DockerContainerStatusSchema = Schema.Struct({
	name: Schema.String,
	state: Schema.String,
	status: Schema.String,
	image: Schema.String,
	port: Schema.Number,
})

export type DockerContainerStatus = typeof DockerContainerStatusSchema.Type

export const DockerUpdateInfoSchema = Schema.Struct({
	hasUpdate: Schema.Boolean,
	image: Schema.String,
	localDigest: Schema.String,
	remoteDigest: Schema.String,
})

export type DockerUpdateInfo = typeof DockerUpdateInfoSchema.Type

export const TorrentTransferStatsSchema = Schema.Struct({
	dlSpeed: Schema.Number,
	upSpeed: Schema.Number,
	activeCount: Schema.Number,
	totalCount: Schema.Number,
})

export type TorrentTransferStats = typeof TorrentTransferStatsSchema.Type

export const BiometricAuthResultSchema = Schema.Struct({
	success: Schema.Boolean,
	error: Schema.optional(Schema.String),
})

export type BiometricAuthResult = typeof BiometricAuthResultSchema.Type

export const DesktopConfigSchema = Schema.Struct({
	quiUrl: Schema.String,
	qbitUrl: Schema.String,
	enableTouchId: Schema.Boolean,
	theme: Schema.String,
	minimizeToTray: Schema.Boolean,
	autoUpdateImages: Schema.Boolean,
})

export type DesktopConfig = typeof DesktopConfigSchema.Type

export const AppReleaseInfoSchema = Schema.Struct({
	hasUpdate: Schema.Boolean,
	currentVersion: Schema.String,
	latestVersion: Schema.String,
	tagName: Schema.String,
	name: Schema.String,
	body: Schema.String,
	htmlUrl: Schema.String,
	downloadUrl: Schema.optional(Schema.String),
	publishedAt: Schema.String,
})

export type AppReleaseInfo = typeof AppReleaseInfoSchema.Type
