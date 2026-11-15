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
  // The details panel's delete handlers call this directly; an immediate
  // refetch there raced the backend's debounced post-delete sync and
  // resurrected deleted rows.
  it("fires a whole-family refetch pair on the given delay", async () => {
    const { queryClient, refetchSpy } = makeQueryClient()

    scheduleTorrentListRefetches(queryClient, 2000)

    await vi.advanceTimersByTimeAsync(1999)
    expect(refetchSpy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(1)
    // Whole ["torrents-list"] family: the all-instances view is keyed under
    // instance id 0, which a row's real instance id can never prefix-match.
    expect(refetchSpy.mock.calls[0][0]).toMatchObject({
      queryKey: ["torrents-list"],
      type: "active",
    })

    await vi.advanceTimersByTimeAsync(3000)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })

  it("a fast action does not shorten a slower action's pending verification window", async () => {
    const { queryClient, refetchSpy } = makeQueryClient()

    // Delete-with-files schedules the 5000/8000 pair...
    scheduleTorrentListRefetches(queryClient, 5000)
    await vi.advanceTimersByTimeAsync(500)
    // ...then a fast action inside that window would replace it with 1000/4000.
    scheduleTorrentListRefetches(queryClient, 1000)

    // The fast action's first refetch runs on its own delay (T+1500)...
    await vi.advanceTimersByTimeAsync(1000)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    // ...but the final pass holds the delete's original T+8000 deadline
    // instead of collapsing to the fast action's T+4500.
    await vi.advanceTimersByTimeAsync(6499)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })
})
