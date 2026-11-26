/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

function isWindowsPath(path: string): boolean {
  return /^([a-zA-Z]:|\\\\)/.test(path)
}

export function pathSeparator(path: string): "/" | "\\" {
  return isWindowsPath(path) ? "\\" : "/"
}

export function lastSeparatorIndex(path: string): number {
  const slash = path.lastIndexOf("/")
  return isWindowsPath(path) ? Math.max(slash, path.lastIndexOf("\\")) : slash
}

export function endsWithSeparator(path: string): boolean {
  return path.length > 0 && lastSeparatorIndex(path) === path.length - 1
}

export function normalizeDirectoryPath(dirPath: string): string {
  if (!dirPath) return ""
  if (isWindowsPath(dirPath)) return dirPath.replace(/[\\/]*$/, "\\")
  return (dirPath.startsWith("/") ? dirPath : `/${dirPath}`).replace(/\/*$/, "/")
}
