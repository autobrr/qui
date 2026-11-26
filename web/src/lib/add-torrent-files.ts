/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentFailedFile, AddTorrentResponse } from "@/types"

const ADD_TORRENT_FILE_BATCH_SIZE = 500

type AddRequest = { torrentFiles?: File[] }

type BatchAddResult = {
  added: number
  total: number
  batch: number
  batches: number
  unconfirmed: File[]
  failedFiles: AddTorrentFailedFile[]
}

export class BatchAddError extends Error implements BatchAddResult {
  readonly added: number
  readonly total: number
  readonly batch: number
  readonly batches: number
  readonly unconfirmed: File[]
  readonly failedFiles: AddTorrentFailedFile[]

  constructor(message: string, result: BatchAddResult) {
    super(message)
    this.name = "BatchAddError"
    this.added = result.added
    this.total = result.total
    this.batch = result.batch
    this.batches = result.batches
    this.unconfirmed = result.unconfirmed
    this.failedFiles = result.failedFiles
  }
}

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
  const rejected: File[] = []
  const total: AddTorrentResponse = { message: "", added: 0, failed: 0, failedFiles }
  for (const [index, batch] of batches.entries()) {
    onBatch(index + 1, batches.length)
    let response: AddTorrentResponse
    try {
      response = await add({ ...base, torrentFiles: batch })
    } catch (error) {
      if (index === 0) throw error
      const message = error instanceof Error ? error.message : String(error)
      throw new BatchAddError(message, {
        added: total.added,
        total: files.length,
        batch: index + 1,
        batches: batches.length,
        unconfirmed: [...rejected, ...batches.slice(index).flat()],
        failedFiles,
      })
    }
    total.message = response.message
    total.added += response.added
    total.failed += response.failed
    const batchFailed = response.failedFiles ?? []
    const failedNames = new Set(batchFailed.map(file => file.filename))
    rejected.push(...batch.filter(file => failedNames.has(file.name)))
    failedFiles.push(...batchFailed)
  }
  return total
}
