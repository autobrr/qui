/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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

  const nameAndVersion = text.match(/^([^\s/]+)[\s/]+(\d[\w.-]*)/)
  const name = nameAndVersion ? nameAndVersion[1] : text || (rule?.name ?? "")

  return { icon: rule?.icon ?? null, name, version: nameAndVersion?.[2] ?? "", letter: Array.from(name)[0] ?? "?" }
}

export function peerClientTitle(client?: string, peerIdClient?: string): string {
  return [client, peerIdClient].filter(Boolean).join(" · ")
}
