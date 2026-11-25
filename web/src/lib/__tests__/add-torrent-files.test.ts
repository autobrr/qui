/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { addFilesInBatches } from "@/lib/add-torrent-files"
import type { AddTorrentResponse } from "@/types"
import { describe, expect, it } from "vitest"

type Request = { torrentFiles?: File[]; category?: string }

function files(count: number) {
  return Array.from({ length: count }, (_, i) => new File(["d4:infod4:name1:xee"], `Example.Show.S01E${String(i + 1).padStart(4, "0")}.torrent`))
}

function recorder(failBatch?: number) {
  const calls: Request[] = []
  const progress: Array<[number, number]> = []
  const add = async (request: Request): Promise<AddTorrentResponse> => {
    calls.push(request)
    if (calls.length === failBatch) throw new Error("Failed to add all torrents: qBittorrent is offline")
    return { message: "ok", added: request.torrentFiles?.length ?? 0, failed: 0 }
  }
  const onBatch = (current: number, total: number) => progress.push([current, total])
  return { calls, progress, add, onBatch }
}

describe("addFilesInBatches", () => {
  it("sends 1,200 files as 3 requests of at most 500 with the shared options, reports each batch as it starts, and sums the responses", async () => {
    const { calls, progress, add, onBatch } = recorder()
    const selection = files(1200)
    const response = await addFilesInBatches(add, { category: "tv" }, selection, onBatch)

    expect(calls.map(call => call.torrentFiles?.length)).toEqual([500, 500, 200])
    expect(calls.every(call => call.category === "tv")).toBe(true)
    expect(calls.flatMap(call => call.torrentFiles)).toEqual(selection)
    expect(progress).toEqual([[1, 3], [2, 3], [3, 3]])
    expect(response.added).toBe(1200)
    expect(response.failed).toBe(0)
  })

  it("sends 500 files or fewer as one request", async () => {
    const { calls, add, onBatch } = recorder()
    await addFilesInBatches(add, {}, files(500), onBatch)
    expect(calls).toHaveLength(1)
  })

  it("still sends the other batches when one fails, and counts the files of the failed batch as failed with its error", async () => {
    const { calls, add, onBatch } = recorder(2)
    const selection = files(1200)
    const response = await addFilesInBatches(add, {}, selection, onBatch)

    expect(calls).toHaveLength(3)
    expect(response.added).toBe(700)
    expect(response.failed).toBe(500)
    expect(response.failedFiles).toHaveLength(500)
    expect(response.failedFiles?.[0]).toEqual({ filename: selection[500].name, error: "Failed to add all torrents: qBittorrent is offline" })
  })

  it("fails when every batch fails, so the dialog shows the error and keeps the files", async () => {
    const add = async (): Promise<AddTorrentResponse> => {
      throw new Error("Instance is disabled")
    }
    await expect(addFilesInBatches(add, {}, files(1200), () => {})).rejects.toThrow("Instance is disabled")
  })
})
