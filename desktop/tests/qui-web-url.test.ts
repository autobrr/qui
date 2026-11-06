/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import {
	DEFAULT_QUI_PORT,
	QUI_WEB_LOGIN_URL,
	QUI_WEB_ORIGIN,
} from '../src/shared/constants.js'

describe('Qui web wrapper URL (BR-desktop-07)', () => {
	it('uses localhost origin with the default Qui HTTP port', () => {
		expect(QUI_WEB_ORIGIN).toBe(`http://localhost:${DEFAULT_QUI_PORT}`)
	})

	it('loads the SPA login route on top of that origin', () => {
		expect(QUI_WEB_LOGIN_URL).toBe(`${QUI_WEB_ORIGIN}/login`)
	})
})
