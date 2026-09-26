/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Mirrors IsAbsoluteClientPath in pkg/pathutil/client_path.go. A path that
// starts with a template action is left to the server, which renders it for a
// sample torrent.
export function isVisiblyRelativeMovePath(path: string): boolean {
  const trimmed = path.trim()
  if (trimmed === "" || trimmed.startsWith("{{")) {
    return false
  }
  return !/^([/\\]|[A-Za-z]:[/\\])/.test(trimmed)
}

// Prefixes of the move path errors from validateMovePath in
// internal/api/handlers/automations.go.
const movePathServerErrorPrefixes = ["Move path must be absolute", "Invalid move path template"]

export function isMovePathServerError(message: string): boolean {
  return movePathServerErrorPrefixes.some(prefix => message.startsWith(prefix))
}
