/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { matchPeerClient } from "@/lib/peer-client-icons"

describe("matchPeerClient", () => {
  it.each([
    // rakshasa libTorrent is rTorrent: ruTorrent icon, reported label kept.
    { client: "libTorrent 0.16.27", peerId: "-lt101B-", icon: "rutorrent", label: "libTorrent 0.16.27", letter: "l" },
    { client: "libtorrent/2.0.11", peerId: "-LT20B0-", icon: "libtorrent", label: "libtorrent 2.0.11", letter: "l" },
    // The peer ID form of the same library: lowercase with a space is not rTorrent.
    { client: "libtorrent 2.0.15", peerId: "-LT20F0-", icon: "libtorrent", label: "libtorrent 2.0.15", letter: "l" },
    // Their text contains "libtorrent/", so they must match before the libtorrent rule.
    { client: "Deluge/2.1.1 libtorrent/2.0.10", peerId: "-DE211s-", icon: "deluge", label: "Deluge 2.1.1", letter: "D" },
    { client: "porla/0.52.0 libtorrent/2.1.2.0", peerId: "-PO0k00-", icon: "porla", label: "porla 0.52.0", letter: "p" },
    { client: "", peerId: "-qB5120-", icon: "qbittorrent", label: "qBittorrent", letter: "q" },
    { client: "Flud 1.10", peerId: "-FL1100-", icon: null, label: "Flud 1.10", letter: "F" },
    // An unmatched text falls back to the peer ID for the icon, but keeps its own label.
    { client: "Unknown [-qB5120-]", peerId: "-qB5120-", icon: "qbittorrent", label: "Unknown [-qB5120-]", letter: "U" },
  ])("$client / $peerId", ({ client, peerId, icon, label, letter }) => {
    expect(matchPeerClient(client, peerId)).toEqual({ icon, label, letter })
  })

  it("stays fast on a 1 MiB client text", () => {
    const client = "1".repeat(1 << 20)
    const start = performance.now()
    const { label } = matchPeerClient(client, undefined)
    expect(performance.now() - start).toBeLessThan(50)
    expect(label.length).toBe(128)
  })
})
