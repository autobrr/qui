/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/** Drive-letter and UNC paths, which is what a Windows qBittorrent returns. A backslash elsewhere is a valid Unix file name character. */
function isWindowsPath(path: string): boolean {
  return /^([a-zA-Z]:|\\\\)/.test(path)
}

export function pathSeparator(path: string): "/" | "\\" {
  return isWindowsPath(path) ? "\\" : "/"
}

/** Index of the last separator: "/" on every host, "\" too on a Windows path. */
export function lastSeparatorIndex(path: string): number {
  const slash = path.lastIndexOf("/")
  return isWindowsPath(path) ? Math.max(slash, path.lastIndexOf("\\")) : slash
}

export function endsWithSeparator(path: string): boolean {
  return path.length > 0 && lastSeparatorIndex(path) === path.length - 1
}

/** Normalizes a directory path for a consistent cache key: rooted, with one trailing separator. */
export function normalizeDirectoryPath(dirPath: string): string {
  if (!dirPath) return ""
  if (isWindowsPath(dirPath)) return dirPath.replace(/[\\/]*$/, "\\")
  return (dirPath.startsWith("/") ? dirPath : `/${dirPath}`).replace(/\/*$/, "/")
}
