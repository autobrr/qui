/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentResponse } from "@/types"

type AddRequest = { urls?: string[]; indexerId?: number }

export async function addUrlsByIndexer<T extends AddRequest>(
  add: (request: T) => Promise<AddTorrentResponse>,
  base: T,
  urls: string[],
  indexerByUrl: Readonly<Record<string, number>> = {}
): Promise<AddTorrentResponse> {
  const groups = new Map<number | undefined, string[]>()
  for (const url of urls) {
    const indexerId = indexerByUrl[url]
    groups.set(indexerId, [...(groups.get(indexerId) ?? []), url])
  }
  if (groups.size <= 1) {
    const [indexerId] = groups.keys()
    return add({ ...base, urls, indexerId })
  }

  const total: AddTorrentResponse = { message: "", added: 0, failed: 0, failedURLs: [] }
  const errors: unknown[] = []
  for (const [indexerId, group] of groups) {
    try {
      const response = await add({ ...base, urls: group, indexerId })
      total.message = response.message
      total.added += response.added
      total.failed += response.failed
      total.failedURLs!.push(...(response.failedURLs ?? []))
    } catch (error) {
      errors.push(error)
      const message = error instanceof Error ? error.message : String(error)
      total.failed += group.length
      total.failedURLs!.push(...group.map(url => ({ url, error: message })))
    }
  }
  if (errors.length === groups.size) throw errors[0]
  return total
}
