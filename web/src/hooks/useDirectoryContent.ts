/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"

type UseDirectoryContentOptions = {
  enabled?: boolean
  staleTimeMs?: number
  mode?: "dirs" | "files"
}

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

export function useDirectoryContent(
  instanceId: number,
  dirPath: string,
  options: UseDirectoryContentOptions = {}
) {
  const { enabled = true, staleTimeMs = 30000, mode = "dirs" } = options
  const normalizedPath = normalizeDirectoryPath(dirPath)

  return useQuery<string[]>({
    queryKey: ["directory-content", instanceId, normalizedPath, mode],
    queryFn: ({ signal }) => api.getDirectoryContent(instanceId, normalizedPath, mode, signal),
    staleTime: staleTimeMs,
    enabled: Boolean(enabled && instanceId && normalizedPath),
  })
}
