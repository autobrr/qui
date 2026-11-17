/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { makeTorrent } from "@/test/mockTorrent"
import { transferShareOffer } from "./transfer-share-offer"

describe("transferShareOffer", () => {
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
})
