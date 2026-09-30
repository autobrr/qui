/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Same rule as pathcmp.IsAbsolute; a path starting with a template action is left to the server.
export function isVisiblyRelativeMovePath(path: string): boolean {
  const trimmed = path.trim()
  if (trimmed === "" || trimmed.startsWith("{{")) {
    return false
  }
  return !/^([/\\]|[A-Za-z]:[/\\])/.test(trimmed)
}

// Prefixes of validateMovePath's errors, so the dialog can show them under the field.
const movePathServerErrorPrefixes = ["Move path must be absolute", "Invalid move path template"]

export function isMovePathServerError(message: string): boolean {
  return movePathServerErrorPrefixes.some(prefix => message.startsWith(prefix))
}
