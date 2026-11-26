/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { addFilesInBatches, BatchAddError } from "@/lib/add-torrent-files"
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

  it("stops at a failed batch and keeps the files that no response confirmed, so a retry sends only those", async () => {
    const { calls, add, onBatch } = recorder(2)
    const selection = files(1200)
    const error = await addFilesInBatches(add, {}, selection, onBatch).catch((caught: unknown) => caught)

    expect(calls).toHaveLength(2)
    expect(error).toBeInstanceOf(BatchAddError)
    if (!(error instanceof BatchAddError)) return
    expect(error.added).toBe(500)
    expect(error.total).toBe(1200)
    expect(error.batch).toBe(2)
    expect(error.batches).toBe(3)
    expect(error.message).toBe("Failed to add all torrents: qBittorrent is offline")
    expect(error.unconfirmed).toEqual(selection.slice(500))
  })

  it("keeps the files that an earlier batch rejected, with their errors, when a later batch fails", async () => {
    const selection = files(1200)
    const calls: Request[] = []
    const add = async (request: Request): Promise<AddTorrentResponse> => {
      calls.push(request)
      if (calls.length === 2) throw new Error("HTTP error! status: 504")
      return { message: "ok", added: 499, failed: 1, failedFiles: [{ filename: selection[3].name, error: "torrent file is not valid" }] }
    }
    const error = await addFilesInBatches(add, {}, selection, () => {}).catch((caught: unknown) => caught)

    expect(error).toBeInstanceOf(BatchAddError)
    if (!(error instanceof BatchAddError)) return
    expect(error.added).toBe(499)
    expect(error.failedFiles).toEqual([{ filename: selection[3].name, error: "torrent file is not valid" }])
    expect(error.unconfirmed).toEqual([selection[3], ...selection.slice(500)])
  })

  it("keeps a confirmed file whose name matches a file that another batch rejected out of the retry", async () => {
    const selection = files(1200)
    const twin = new File(["d4:infod4:name1:yee"], selection[3].name)
    selection[600] = twin
    let call = 0
    const add = async (): Promise<AddTorrentResponse> => {
      call += 1
      if (call === 2) return { message: "ok", added: 499, failed: 1, failedFiles: [{ filename: twin.name, error: "torrent file is not valid" }] }
      if (call === 3) throw new Error("HTTP error! status: 504")
      return { message: "ok", added: 500, failed: 0 }
    }
    const error = await addFilesInBatches(add, {}, selection, () => {}).catch((caught: unknown) => caught)

    expect(error).toBeInstanceOf(BatchAddError)
    if (!(error instanceof BatchAddError)) return
    expect(error.unconfirmed).toEqual([twin, ...selection.slice(1000)])
    expect(error.unconfirmed).not.toContain(selection[3])
  })

  it("throws the request error unchanged when the first batch fails", async () => {
    const { add, onBatch } = recorder(1)
    const error = await addFilesInBatches(add, {}, files(1200), onBatch).catch((caught: unknown) => caught)
    expect(error).not.toBeInstanceOf(BatchAddError)
    expect(error).toBeInstanceOf(Error)
  })
})
