/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { resetPendingListRefetchesForTests, scheduleTorrentListRefetches } from "../bulk-action-requests"

function makeQueryClient() {
  const queryClient = new QueryClient()
  const refetchSpy = vi.spyOn(queryClient, "refetchQueries").mockResolvedValue(undefined)
  return { queryClient, refetchSpy }
}

beforeEach(() => {
  vi.useFakeTimers()
  resetPendingListRefetchesForTests()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("scheduleTorrentListRefetches", () => {
  it("fires a whole-family refetch pair on the given delay", async () => {
    const { queryClient, refetchSpy } = makeQueryClient()

    scheduleTorrentListRefetches(queryClient, 2000)

    await vi.advanceTimersByTimeAsync(1999)
    expect(refetchSpy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(1)
    expect(refetchSpy.mock.calls[0][0]).toMatchObject({
      queryKey: ["torrents-list"],
      type: "active",
    })

    await vi.advanceTimersByTimeAsync(3000)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })

  it("a fast action does not shorten a slower action's pending verification window", async () => {
    const { queryClient, refetchSpy } = makeQueryClient()

    scheduleTorrentListRefetches(queryClient, 5000)
    await vi.advanceTimersByTimeAsync(500)
    scheduleTorrentListRefetches(queryClient, 1000)

    await vi.advanceTimersByTimeAsync(1000)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(6499)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })
})
