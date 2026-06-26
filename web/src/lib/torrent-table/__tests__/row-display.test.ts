/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getRowBackgroundClass, getStatusBadgeProps, getStatusBadgeVariant } from "@/lib/torrent-table/row-display"
import { makeTorrent } from "@/test/mockTorrent"
import type { TFunction } from "i18next"
import { describe, expect, it } from "vitest"

// Passthrough translator: honors an explicit defaultValue (as getStateLabel
// uses), otherwise echoes the key so assertions can match on the key string.
const t = ((key: string, opts?: { defaultValue?: string }) =>
  opts?.defaultValue ?? key) as unknown as TFunction

describe("getRowBackgroundClass", () => {
  it("uses the accent background when the row is selected, regardless of zebra parity", () => {
    expect(getRowBackgroundClass(true, false, 0)).toBe("bg-accent")
    expect(getRowBackgroundClass(true, false, 1)).toBe("bg-accent")
    expect(getRowBackgroundClass(false, true, 0)).toBe("bg-accent")
  })

  it("selection takes precedence over the zebra stripe", () => {
    // Odd index would normally be muted, but selection wins.
    expect(getRowBackgroundClass(true, false, 1)).toBe("bg-accent")
  })

  it("applies the zebra stripe to odd rows when unselected", () => {
    expect(getRowBackgroundClass(false, false, 1)).toBe("bg-muted/40")
    expect(getRowBackgroundClass(false, false, 3)).toBe("bg-muted/40")
  })

  it("returns no background for unselected even rows", () => {
    expect(getRowBackgroundClass(false, false, 0)).toBe("")
    expect(getRowBackgroundClass(false, false, 2)).toBe("")
  })
})

describe("getStatusBadgeVariant", () => {
  it("maps active transfer states to the default variant", () => {
    expect(getStatusBadgeVariant("downloading")).toBe("default")
    expect(getStatusBadgeVariant("uploading")).toBe("default")
  })

  it("maps stalled and paused states to the secondary variant", () => {
    expect(getStatusBadgeVariant("stalledDL")).toBe("secondary")
    expect(getStatusBadgeVariant("stalledUP")).toBe("secondary")
    expect(getStatusBadgeVariant("pausedDL")).toBe("secondary")
    expect(getStatusBadgeVariant("pausedUP")).toBe("secondary")
  })

  it("maps error states to the destructive variant", () => {
    expect(getStatusBadgeVariant("error")).toBe("destructive")
    expect(getStatusBadgeVariant("missingFiles")).toBe("destructive")
  })

  it("falls back to the outline variant for unknown states", () => {
    expect(getStatusBadgeVariant("stoppedUP")).toBe("outline")
    expect(getStatusBadgeVariant("someFutureState")).toBe("outline")
  })
})

describe("getStatusBadgeProps", () => {
  it("derives variant from state and leaves className empty when tracker health is unsupported", () => {
    const torrent = makeTorrent({ state: "downloading", tracker_health: "tracker_down" })
    const props = getStatusBadgeProps(torrent, false, t)
    expect(props.variant).toBe("default")
    expect(props.className).toBe("")
    // Label comes from getStateLabel's English fallback (not a tracker-health override).
    expect(props.label).toBe("Downloading")
  })

  it("overrides label/variant/className for tracker_down when tracker health is supported", () => {
    const torrent = makeTorrent({ state: "downloading", tracker_health: "tracker_down" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe("tableColumns.trackerDown")
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-yellow-500")
  })

  it("overrides for tracker_error", () => {
    const torrent = makeTorrent({ state: "stalledUP", tracker_health: "tracker_error" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe("tableColumns.trackerError")
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-orange-500")
  })

  it("overrides for unregistered", () => {
    const torrent = makeTorrent({ state: "uploading", tracker_health: "unregistered" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe("tableColumns.unregistered")
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-destructive")
  })

  it("ignores a healthy tracker_health value and keeps the state-derived badge", () => {
    const torrent = makeTorrent({ state: "uploading" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.variant).toBe("default")
    expect(props.className).toBe("")
  })
})
