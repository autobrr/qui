/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { findOutsideFile } from "@/lib/css-outside-files"
import { endCustomThemePreview, paintCustomThemePreview } from "@/utils/theme"
import { findThemeCssProblem } from "@/utils/themeParser"

declare const checked: unique symbol
export type PreviewableThemeCss = string & { readonly [checked]: true }

export type ThemeCssProblem = "empty" | "missing-root" | "missing-dark"

export type PreviewProblem =
  | { kind: ThemeCssProblem }
  | { kind: "outside-file" | "unreadable"; line: number }

export function findThemeProblem(css: string): ThemeCssProblem | null {
  return css.trim() === "" ? "empty" : findThemeCssProblem(css)
}

let lastCheck: { css: string; problem: PreviewProblem | null } | null = null

export function findPreviewProblem(css: string): PreviewProblem | null {
  if (lastCheck?.css === css) return lastCheck.problem
  const theme = findThemeProblem(css)
  const problem = theme ? { kind: theme } : findOutsideFile(css)
  lastCheck = { css, problem }
  return problem
}

export function previewCustomTheme(css: string): PreviewProblem | null {
  const problem = findPreviewProblem(css)
  if (!problem) paintCustomThemePreview(css as PreviewableThemeCss)
  return problem
}

export { endCustomThemePreview }
