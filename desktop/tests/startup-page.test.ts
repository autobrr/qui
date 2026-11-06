/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from 'vitest'
import { startupPageDocument } from '../src/main/startup-page.js'

describe('Startup page (BR-desktop-09)', () => {
	it('paints an opaque Minimal background, the spinner, and a process code block', () => {
		const html = startupPageDocument(
			'Starting Qui…',
			'Ensuring Docker containers (qui, qbit) are running…',
			true,
		)

		expect(html).toContain('background:#252525')
		expect(html).not.toContain('background:transparent')
		expect(html).toContain('fill="#faf4ff"')
		expect(html).toContain('attributeName="opacity"')
		expect(html).toContain(
			'<pre class="process-code"><code>Ensuring Docker containers (qui, qbit) are running…</code></pre>',
		)
	})

	it('uses the light Minimal fill when the OS appearance is light', () => {
		const html = startupPageDocument('Starting Qui…', 'Waiting…', false)
		expect(html).toContain('background:#ffffff')
		expect(html).not.toContain('background:#252525')
	})

	it('escapes the process log so a docker error cannot break the page', () => {
		const html = startupPageDocument(
			'Failed to start Qui',
			'docker binary not found <script>',
			true,
		)
		expect(html).toContain(
			'<pre class="process-code"><code>docker binary not found &lt;script&gt;</code></pre>',
		)
		expect(html).not.toContain('<script>')
	})
})
