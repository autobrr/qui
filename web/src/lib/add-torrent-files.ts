/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentFailedFile, AddTorrentResponse } from "@/types"

const ADD_TORRENT_FILE_BATCH_SIZE = 500

type AddRequest = { torrentFiles?: File[] }

export async function addFilesInBatches<T extends AddRequest>(
  add: (request: T) => Promise<AddTorrentResponse>,
  base: T,
  files: File[],
  onBatch: (current: number, total: number) => void
): Promise<AddTorrentResponse> {
  const batches: File[][] = []
  for (let start = 0; start < files.length; start += ADD_TORRENT_FILE_BATCH_SIZE) {
    batches.push(files.slice(start, start + ADD_TORRENT_FILE_BATCH_SIZE))
  }
  if (batches.length <= 1) {
    onBatch(1, 1)
    return add({ ...base, torrentFiles: files })
  }

  const failedFiles: AddTorrentFailedFile[] = []
  const total: AddTorrentResponse = { message: "", added: 0, failed: 0, failedFiles }
  const errors: unknown[] = []
  for (const [index, batch] of batches.entries()) {
    onBatch(index + 1, batches.length)
    try {
      const response = await add({ ...base, torrentFiles: batch })
      total.message = response.message
      total.added += response.added
      total.failed += response.failed
      failedFiles.push(...(response.failedFiles ?? []))
    } catch (error) {
      errors.push(error)
      const message = error instanceof Error ? error.message : String(error)
      total.failed += batch.length
      failedFiles.push(...batch.map(file => ({ filename: file.name, error: message })))
    }
  }
  if (errors.length === batches.length) throw errors[0]
  return total
}
