/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { addUrlsByIndexer } from "@/lib/add-torrent-urls"
import type { AddTorrentResponse } from "@/types"
import { describe, expect, it } from "vitest"

type Request = { urls?: string[]; indexerId?: number; category?: string }

function recorder(fail?: number) {
  const calls: Request[] = []
  const add = async (request: Request): Promise<AddTorrentResponse> => {
    calls.push(request)
    if (fail !== undefined && request.indexerId === fail) throw new Error("indexer down")
    return { message: "ok", added: request.urls?.length ?? 0, failed: 0 }
  }
  return { calls, add }
}

describe("addUrlsByIndexer", () => {
  it("sends the URLs of each indexer in their own request with the shared options, and reports a failed request while it adds the rest", async () => {
    const { calls, add } = recorder()
    const response = await addUrlsByIndexer(add, { category: "movies" }, ["a", "b", "c"], { a: 1, b: 2, c: 1 })
    expect(calls).toEqual([
      { category: "movies", urls: ["a", "c"], indexerId: 1 },
      { category: "movies", urls: ["b"], indexerId: 2 },
    ])
    expect(response.added).toBe(3)
    expect(response.failed).toBe(0)

    calls.length = 0
    await addUrlsByIndexer(add, {}, ["a", "b"], { a: 4, b: 4 })
    await addUrlsByIndexer(add, {}, ["typed"])
    expect(calls).toEqual([{ urls: ["a", "b"], indexerId: 4 }, { urls: ["typed"], indexerId: undefined }])

    const failing = recorder(2)
    const partial = await addUrlsByIndexer(failing.add, {}, ["a", "b", "c"], { a: 1, b: 2, c: 2 })
    expect(partial.added).toBe(1)
    expect(partial.failed).toBe(2)
    expect(partial.failedURLs).toEqual([{ url: "b", error: "indexer down" }, { url: "c", error: "indexer down" }])
  })

  it("fails when every request fails, so the dialog stays open", async () => {
    const add = async (): Promise<AddTorrentResponse> => {
      throw new Error("indexer down")
    }
    await expect(addUrlsByIndexer(add, {}, ["a", "b"], { a: 1, b: 2 })).rejects.toThrow("indexer down")
  })
})
