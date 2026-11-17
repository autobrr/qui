/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { makeTorrent } from "@/test/mockTorrent"
import { transferShareOffer } from "./transfer-share-offer"

const demo = vi.hoisted(() => ({ isDemo: false }))
vi.mock("@/lib/demo", () => ({ get isDemo() { return demo.isDemo } }))

describe("transferShareOffer", () => {
  afterEach(() => {
    demo.isDemo = false
  })

  it("offers Transfer and Share for one torrent", () => {
    expect(transferShareOffer([makeTorrent({ hash: "a" })], 1, [3])).toEqual({ transfer: true, share: true })
  })

  it("offers only Share for several torrents on one instance", () => {
    expect(transferShareOffer([makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })], 2, [3])).toEqual({ transfer: false, share: true })
  })

  it("offers neither for torrents on several instances", () => {
    expect(transferShareOffer([makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })], 2, [3, 4])).toEqual({ transfer: false, share: false })
  })

  it("offers neither when the menu cannot name each torrent of the selection", () => {
    expect(transferShareOffer([makeTorrent({ hash: "a" })], 40, [3])).toEqual({ transfer: false, share: false })
  })

  it("offers neither for a helper torrent of a Transfer or a Share", () => {
    expect(transferShareOffer([makeTorrent({ hash: "a", qui_helper: "transfer" })], 1, [3])).toEqual({ transfer: false, share: false })
  })

  // regression: #4242
  it("offers Share for at most 100 torrents, the most that one Share request takes", () => {
    const torrents = (count: number) => Array.from({ length: count }, (_, i) => makeTorrent({ hash: `h${i}` }))
    expect(transferShareOffer(torrents(100), 100, [3])).toEqual({ transfer: false, share: true })
    expect(transferShareOffer(torrents(101), 101, [3])).toEqual({ transfer: false, share: false })
  })

  // regression: #4242
  it("offers neither in the demo, which serves no Transfer or Share request", () => {
    demo.isDemo = true
    expect(transferShareOffer([makeTorrent({ hash: "a" })], 1, [3])).toEqual({ transfer: false, share: false })
  })
})
