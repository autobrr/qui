/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedOrchestration } from "@/hooks/torrent-table/useCrossSeedOrchestration"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/hooks/useCrossSeedWarning", () => ({ useCrossSeedWarning: vi.fn() }))
vi.mock("@/hooks/useCrossSeedBlocklistActions", () => ({ useCrossSeedBlocklistActions: vi.fn() }))

import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"

const mockWarning = vi.mocked(useCrossSeedWarning)
const mockBlocklist = vi.mocked(useCrossSeedBlocklistActions)

function setAffected(torrents: Torrent[]) {
  mockWarning.mockReturnValue({ affectedTorrents: torrents } as unknown as ReturnType<typeof useCrossSeedWarning>)
}

beforeEach(() => {
  setAffected([])
  mockBlocklist.mockReturnValue({ blockCrossSeedHashes: vi.fn() } as unknown as ReturnType<typeof useCrossSeedBlocklistActions>)
})

function render(contextTorrents: Torrent[], blockCrossSeeds: boolean) {
  return renderHook(() =>
    useCrossSeedOrchestration({ instanceId: 1, instanceName: "inst", contextTorrents, blockCrossSeeds })
  )
}

describe("useCrossSeedOrchestration", () => {
  it("flags the cross-seed tag from a context torrent", () => {
    const { result } = render([makeTorrent({ tags: "cross-seed" })], false)
    expect(result.current.hasCrossSeedTag).toBe(true)
  })

  it("flags the cross-seed tag from a warning-affected torrent even if context has none", () => {
    setAffected([makeTorrent({ tags: "cross-seed" })])
    const { result } = render([makeTorrent({ tags: "linux" })], false)
    expect(result.current.hasCrossSeedTag).toBe(true)
  })

  it("does not flag when neither context nor affected torrents are tagged", () => {
    const { result } = render([makeTorrent({ tags: "linux" })], false)
    expect(result.current.hasCrossSeedTag).toBe(false)
  })

  it("only blocks cross-seeds when tagged AND blockCrossSeeds is enabled", () => {
    expect(render([makeTorrent({ tags: "cross-seed" })], true).result.current.shouldBlockCrossSeeds).toBe(true)
    expect(render([makeTorrent({ tags: "cross-seed" })], false).result.current.shouldBlockCrossSeeds).toBe(false)
    expect(render([makeTorrent({ tags: "linux" })], true).result.current.shouldBlockCrossSeeds).toBe(false)
  })
})
