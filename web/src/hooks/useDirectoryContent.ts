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

export function useDirectoryContent(
  instanceId: number,
  dirPath: string,
  options: UseDirectoryContentOptions = {}
) {
  const { enabled = true, staleTimeMs = 30000, mode = "dirs" } = options

  // Normalize the path for consistent cache keys
  let normalizedPath = ""
  if (dirPath) {
    const withLeadingSlash = dirPath.startsWith("/") ? dirPath : `/${dirPath}`
    normalizedPath = withLeadingSlash.replace(/\/*$/, "/")
  }

  return useQuery<string[]>({
    queryKey: ["directory-content", instanceId, normalizedPath, mode],
    queryFn: ({ signal }) => api.getDirectoryContent(instanceId, normalizedPath, mode, signal),
    staleTime: staleTimeMs,
    enabled: Boolean(enabled && instanceId && normalizedPath),
  })
}
