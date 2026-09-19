/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/** Separator a path uses: backslash for drive-letter, UNC, and backslash paths, which is what a Windows qBittorrent returns. */
export function pathSeparator(path: string): "/" | "\\" {
  return path.includes("\\") || /^[a-zA-Z]:/.test(path) ? "\\" : "/"
}

/** Normalizes a directory path for a consistent cache key: rooted, with one trailing separator. */
export function normalizeDirectoryPath(dirPath: string): string {
  if (!dirPath) return ""
  const separator = pathSeparator(dirPath)
  const rooted = separator === "\\" || dirPath.startsWith("/") ? dirPath : `/${dirPath}`
  return rooted.replace(/[\\/]*$/, separator)
}
