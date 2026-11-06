/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
	MINIMAL_DARK_BACKGROUND,
	MINIMAL_LIGHT_BACKGROUND,
} from './window-material.js'

export const LOADING_SPINNER_SVG = `<svg width="60" height="60" viewBox="0 0 50 50"><g><g transform="rotate(0 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0s" repeatCount="indefinite"></animate></line></g><g transform="rotate(60 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.3s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.3s" repeatCount="indefinite"></animate></line></g><g transform="rotate(120 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.6s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.6s" repeatCount="indefinite"></animate></line></g><g transform="rotate(180 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="0.8999999999999999s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="0.8999999999999999s" repeatCount="indefinite"></animate></line></g><g transform="rotate(240 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="1.2s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="1.2s" repeatCount="indefinite"></animate></line></g><g transform="rotate(300 25 25)"><rect x="22" y="5" width="6" height="6" fill="#faf4ff" opacity="0.8"><animate attributeName="opacity" values="0.8;0.2;0.8" dur="2s" begin="1.5s" repeatCount="indefinite"></animate></rect><line x1="25" y1="11" x2="25" y2="20" stroke="#faf4ff" stroke-width="2" opacity="0.5"><animate attributeName="opacity" values="0.5;0.1;0.5" dur="2s" begin="1.5s" repeatCount="indefinite"></animate></line></g><circle cx="25" cy="25" r="8" fill="none" stroke="#faf4ff" stroke-width="2"><animate attributeName="r" values="8;9;8" dur="2s" repeatCount="indefinite"></animate></circle></g></svg>`

function escapeHtml(value: string): string {
	return value
		.replaceAll('&', '&amp;')
		.replaceAll('<', '&lt;')
		.replaceAll('>', '&gt;')
}

export function startupPageDocument(
	message: string,
	processInfo: string,
	prefersDark: boolean,
): string {
	const background = prefersDark
		? MINIMAL_DARK_BACKGROUND
		: MINIMAL_LIGHT_BACKGROUND
	const safeMessage = escapeHtml(message)
	const safeProcess = escapeHtml(processInfo)

	return `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
html,body{margin:0;height:100%;background:${background};color:#f0f6fc;font-family:-apple-system,BlinkMacSystemFont,system-ui,sans-serif;display:flex;align-items:center;justify-content:center}
.card{background:rgba(22,27,34,.92);border:1px solid rgba(255,255,255,.14);border-radius:20px;padding:32px 36px;text-align:center;max-width:440px;box-shadow:0 20px 40px rgba(0,0,0,.45)}
.spinner{display:flex;align-items:center;justify-content:center;margin-bottom:16px}
h1{font-size:24px;margin:0 0 10px;font-weight:650;letter-spacing:-0.02em}
p{margin:0;color:#c9d1d9;font-size:14px;line-height:1.5}
.process-code{margin:16px 0 0;padding:10px 14px;background:rgba(13,17,23,.85);border:1px solid rgba(255,255,255,.1);border-radius:10px;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:12px;color:#a5d6ff;text-align:left;overflow-x:auto;max-height:120px;white-space:pre-wrap;word-break:break-all}
</style></head><body><div class="card"><div class="spinner">${LOADING_SPINNER_SVG}</div><h1>Qui</h1><p>${safeMessage}</p><pre class="process-code"><code>${safeProcess}</code></pre></div></body></html>`
}

export function startupPageUrl(
	message: string,
	processInfo: string,
	prefersDark: boolean,
): string {
	return `data:text/html;charset=utf-8,${encodeURIComponent(
		startupPageDocument(message, processInfo, prefersDark),
	)}`
}
