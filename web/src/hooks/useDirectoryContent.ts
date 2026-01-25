/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"

type UseDirectoryContentOptions = {
  enabled?: boolean
  staleTimeMs?: number
}

export function useDirectoryContent(
  instanceId: number,
  dirPath: string,
  options: UseDirectoryContentOptions = {}
) {
  const { enabled = true, staleTimeMs = 30000 } = options

  // Normalize the path for consistent cache keys
  const normalizedPath = dirPath
    ? (dirPath.startsWith("/") ? dirPath : `/${dirPath}`).replace(/\/*$/, "/")
    : ""

  return useQuery<string[]>({
    queryKey: ["directory-content", instanceId, normalizedPath],
    queryFn: () => api.getDirectoryContent(instanceId, normalizedPath),
    staleTime: staleTimeMs,
    enabled: Boolean(enabled && instanceId && normalizedPath),
  })
}
