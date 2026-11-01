/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Matches the client text first, then the peer ID prefix. Order matters:
// Deluge, FrostWire and Porla texts contain "libtorrent/", so they come before
// the libtorrent rule. Capital "libTorrent " is rakshasa libTorrent (rTorrent).
// The icon names are the list in internal/services/peerclienticons/icons.go.
const RULES: { name: string; client: RegExp; peerId: string; icon?: string }[] = [
  { name: "qBittorrent", client: /^qBittorrent/, peerId: "-qB", icon: "qbittorrent" },
  { name: "Transmission", client: /^Transmission/, peerId: "-TR", icon: "transmission" },
  { name: "Deluge", client: /^Deluge/, peerId: "-DE", icon: "deluge" },
  { name: "FrostWire", client: /^FrostWire/, peerId: "-FW" },
  { name: "Porla", client: /^porla\//, peerId: "-PO", icon: "porla" },
  { name: "rTorrent", client: /^(libTorrent|rTorrent) /, peerId: "-lt", icon: "rutorrent" },
  { name: "libtorrent", client: /^libtorrent[/ ]/, peerId: "-LT", icon: "libtorrent" },
  { name: "BiglyBT", client: /^BiglyBT/, peerId: "-BI", icon: "biglybt" },
  { name: "WebTorrent", client: /^WebTorrent/, peerId: "-WW", icon: "webtorrent" },
  { name: "PicoTorrent", client: /^PicoTorrent/, peerId: "-PI", icon: "picotorrent" },
  { name: "LibreTorrent", client: /^LibreTorrent/, peerId: "-Lr", icon: "libretorrent" },
  { name: "KTorrent", client: /^KTorrent/, peerId: "-KT", icon: "ktorrent" },
  { name: "µTorrent", client: /^(µ|u)Torrent/i, peerId: "-UT", icon: "utorrent" },
  { name: "BitTorrent", client: /^BitTorrent/, peerId: "-BT", icon: "bittorrent" },
  { name: "BitComet", client: /^BitComet/, peerId: "-BC", icon: "bitcomet" },
  { name: "Tixati", client: /^Tixati/, peerId: "TIX" },
  { name: "aria2", client: /^aria2/, peerId: "A2-" },
]

// A peer in a public swarm chooses the text, so no pattern ever sees more than this.
const MAX_CLIENT_LENGTH = 128

export interface PeerClientMatch {
  icon: string | null
  name: string
  version: string
  letter: string
}

export function matchPeerClient(client: string | undefined, peerIdClient: string | undefined): PeerClientMatch {
  const text = (client ?? "").slice(0, MAX_CLIENT_LENGTH).trim()

  const rule = (text ? RULES.find((r) => r.client.test(text)) : undefined) ??
    (peerIdClient ? RULES.find((r) => peerIdClient.startsWith(r.peerId)) : undefined)

  // "Deluge/2.1.1 libtorrent/2.0.10" gives "Deluge" and "2.1.1": the reported name and its version.
  // Only a peer that sends no text gets the name of its peer ID rule.
  const nameAndVersion = text.match(/^([^\s/]+)[\s/]+(\d[\w.-]*)/)
  const name = nameAndVersion ? nameAndVersion[1] : text || (rule?.name ?? "")

  return { icon: rule?.icon ?? null, name, version: nameAndVersion?.[2] ?? "", letter: name.charAt(0) || "?" }
}

/** The full claim of the peer, for a tooltip: `Deluge/2.1.1 libtorrent/2.0.10 · -DE211s-`. */
export function peerClientTitle(client?: string, peerIdClient?: string): string {
  return [client, peerIdClient].filter(Boolean).join(" · ")
}
