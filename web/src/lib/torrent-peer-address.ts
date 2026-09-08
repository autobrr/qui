import type { SortedPeer, TorrentPeer } from "@/types"

export function getPeerDisplayAddress(peer: SortedPeer, incognitoMode: boolean): string {
  return incognitoMode ? "192.168.x.x:xxxxx" : peer.key
}

export function canBanPeer(peer: TorrentPeer): boolean {
  return peer.ip !== undefined && peer.port !== undefined
}
