/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { findOutsideFile } from "@/lib/css-outside-files"
import { endCustomThemePreview, paintCustomThemePreview } from "@/utils/theme"
import { findThemeCssProblem } from "@/utils/themeParser"

// Only previewCustomTheme makes this type, so theme.ts paints no CSS that skipped the checks.
declare const checked: unique symbol
export type PreviewableThemeCss = string & { readonly [checked]: true }

export type ThemeCssProblem = "empty" | "missing-root" | "missing-dark"

export type PreviewProblem =
  | { kind: ThemeCssProblem }
  | { kind: "outside-file" | "unreadable"; line: number }

/** Why qui cannot use css as a theme, or null. */
export function findThemeProblem(css: string): ThemeCssProblem | null {
  return css.trim() === "" ? "empty" : findThemeCssProblem(css)
}

let lastCheck: { css: string; problem: PreviewProblem | null } | null = null

/**
 * Why css cannot show in the preview, or null. The preview also refuses CSS
 * that can load a file from another website, with the same rule as the
 * server, so the browser never loads that file while the user edits.
 */
export function findPreviewProblem(css: string): PreviewProblem | null {
  // The editor asks for the status line and then previews the same css, so the last answer is kept.
  if (lastCheck?.css === css) return lastCheck.problem
  const theme = findThemeProblem(css)
  const problem = theme ? { kind: theme } : findOutsideFile(css)
  lastCheck = { css, problem }
  return problem
}

/**
 * Shows css as the page theme in this browser only, until
 * endCustomThemePreview. A later theme, mode, or variation change keeps the
 * preview on the page. It writes no stored theme or boot cache, so a reload
 * also ends it. When css has a problem, it leaves the page as it is and
 * returns the problem.
 */
export function previewCustomTheme(css: string): PreviewProblem | null {
  const problem = findPreviewProblem(css)
  if (!problem) paintCustomThemePreview(css as PreviewableThemeCss)
  return problem
}

export { endCustomThemePreview }
