/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const APP_NAME = 'Qui'
export const BUNDLE_ID = 'com.autobrr.qui.desktop'
export const DEFAULT_QUI_PORT = 7476
/** Qui HTTP origin (Docker maps host port to container). */
export const QUI_WEB_ORIGIN = `http://localhost:${DEFAULT_QUI_PORT}`
/**
 * SPA entry for sign-in; first-run registration uses `/setup` (Login redirects when
 * `checkSetupRequired` is true).
 */
export const QUI_WEB_LOGIN_URL = `${QUI_WEB_ORIGIN}/login`
export const DEFAULT_QBIT_PORT = 8080
/** Local image built from this repo so the desktop chrome ships. */
export const QUI_IMAGE = 'qui-desktop:local'
export const QBIT_IMAGE = 'lscr.io/linuxserver/qbittorrent:latest'
export const QUI_CONTAINER_NAME = 'qui'
export const QBIT_CONTAINER_NAME = 'qbittorrent-nox'
export const GITHUB_REPO = 'Johann-Goncalves-Pereira/Qui'
export const CURRENT_APP_VERSION = '1.0.0'
