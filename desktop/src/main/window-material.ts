/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/** Minimal dark background, oklch(0.145 0 0). */
export const MINIMAL_DARK_BACKGROUND = '#252525'
/** Minimal light background, oklch(1 0 0). */
export const MINIMAL_LIGHT_BACKGROUND = '#ffffff'
/** Clear fill so macOS vibrancy shows through the frosted page. */
export const GLASS_WINDOW_BACKGROUND = '#00000000'

export interface WindowMaterial {
	vibrancy: 'hud' | null
	backgroundColor: string
}

/**
 * The window fill stays clear so glass can turn on and off without getting
 * stuck on a solid color. HUD vibrancy blurs the desktop behind the page
 * while the slider is below 100%. At 100% the page itself is opaque.
 */
export function windowMaterialForGlass(enabled: boolean): WindowMaterial {
	return {
		vibrancy: enabled ? 'hud' : null,
		backgroundColor: GLASS_WINDOW_BACKGROUND,
	}
}
