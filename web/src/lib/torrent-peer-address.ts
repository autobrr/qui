import type { SortedPeer, TorrentPeer } from "@/types"

/** The address that incognito mode shows in place of a peer address. */
export const INCOGNITO_PEER_ADDRESS = "192.168.x.x:xxxxx"

export function getPeerDisplayAddress(peer: SortedPeer, incognitoMode: boolean): string {
  return incognitoMode ? INCOGNITO_PEER_ADDRESS : peer.key
}

export function canBanPeer(peer: TorrentPeer): boolean {
  return peer.ip !== undefined && peer.port !== undefined
}
