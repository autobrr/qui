/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentFailedFile, AddTorrentResponse } from "@/types"

const ADD_TORRENT_FILE_BATCH_SIZE = 500

type AddRequest = { torrentFiles?: File[] }

export class BatchAddError extends Error {
  readonly added: number
  readonly total: number
  readonly batch: number
  readonly batches: number
  readonly unconfirmed: File[]

  constructor(message: string, added: number, total: number, batch: number, batches: number, unconfirmed: File[]) {
    super(message)
    this.name = "BatchAddError"
    this.added = added
    this.total = total
    this.batch = batch
    this.batches = batches
    this.unconfirmed = unconfirmed
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
  const total: AddTorrentResponse = { message: "", added: 0, failed: 0, failedFiles }
  for (const [index, batch] of batches.entries()) {
    onBatch(index + 1, batches.length)
    let response: AddTorrentResponse
    try {
      response = await add({ ...base, torrentFiles: batch })
    } catch (error) {
      if (index === 0) throw error
      const message = error instanceof Error ? error.message : String(error)
      throw new BatchAddError(message, total.added, files.length, index + 1, batches.length, files.slice(index * ADD_TORRENT_FILE_BATCH_SIZE))
    }
    total.message = response.message
    total.added += response.added
    total.failed += response.failed
    failedFiles.push(...(response.failedFiles ?? []))
  }
  return total
}
