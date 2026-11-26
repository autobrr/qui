import type { SortedPeer } from "@/types"
import { describe, expect, it } from "vitest"
import { canBanPeer, getPeerDisplayAddress } from "./torrent-peer-address"

const peer = (value: Partial<SortedPeer> & Pick<SortedPeer, "key">): SortedPeer => ({
  progress: 0,
  ...value,
})

describe("torrent peer addresses", () => {
  it.each([
    { name: "IPv4", value: peer({ key: "198.51.100.7:6881", ip: "198.51.100.7", port: 6881 }), bannable: true },
    { name: "IPv6", value: peer({ key: "[2001:db8::7]:6881", ip: "2001:db8::7", port: 6881 }), bannable: true },
    { name: "I2P", value: peer({ key: "exampledestination.b32.i2p" }), bannable: false },
  ])("uses the canonical $name key", ({ value, bannable }) => {
    expect(getPeerDisplayAddress(value, false)).toBe(value.key)
    expect(canBanPeer(value)).toBe(bannable)
  })

  it("masks every address kind in incognito mode", () => {
    const value = peer({ key: "exampledestination.b32.i2p" })
    expect(getPeerDisplayAddress(value, true)).toBe("192.168.x.x:xxxxx")
  })
})
