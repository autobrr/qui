/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getRowBackgroundClass, getStatusBadgeProps, getStatusBadgeVariant, TRACKER_HEALTH_DISPLAY, type TrackerHealth } from "@/lib/torrent-table/row-display"
import i18n from "@/i18n"
import enTorrents from "@/i18n/locales/en/torrents.json"
import { makeTorrent } from "@/test/mockTorrent"
import { describe, expect, it } from "vitest"

const t = i18n.getFixedT("en", "torrents")

describe("getRowBackgroundClass", () => {
  it("highlights a selected row the same way on every stripe", () => {
    const selected = getRowBackgroundClass(true, false, 0)
    expect(getRowBackgroundClass(true, false, 1)).toBe(selected)
    expect(getRowBackgroundClass(false, true, 0)).toBe(selected)
    expect(selected).not.toBe(getRowBackgroundClass(false, false, 0))
    expect(selected).not.toBe(getRowBackgroundClass(false, false, 1))
  })

  it("stripes odd unselected rows", () => {
    expect(getRowBackgroundClass(false, false, 0)).toBe("")
    expect(getRowBackgroundClass(false, false, 2)).toBe("")
    expect(getRowBackgroundClass(false, false, 1)).not.toBe("")
    expect(getRowBackgroundClass(false, false, 3)).toBe(getRowBackgroundClass(false, false, 1))
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

  it("maps queued states to the secondary variant", () => {
    expect(getStatusBadgeVariant("queuedDL")).toBe("secondary")
    expect(getStatusBadgeVariant("queuedUP")).toBe("secondary")
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
    expect(props.label).toBe("Downloading")
  })

  it("overrides label/variant/className for tracker_down when tracker health is supported", () => {
    const torrent = makeTorrent({ state: "downloading", tracker_health: "tracker_down" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe(enTorrents.tableColumns.trackerDown)
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-warning")
  })

  it("overrides for tracker_error", () => {
    const torrent = makeTorrent({ state: "stalledUP", tracker_health: "tracker_error" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe(enTorrents.tableColumns.trackerError)
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-purple-500")
    expect(props.iconClass).toBe("text-purple-500")
  })

  it("overrides for unregistered", () => {
    const torrent = makeTorrent({ state: "uploading", tracker_health: "unregistered" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.label).toBe(enTorrents.tableColumns.unregistered)
    expect(props.variant).toBe("outline")
    expect(props.className).toContain("text-destructive")
  })

  it("ignores a healthy tracker_health value and keeps the state-derived badge", () => {
    const torrent = makeTorrent({ state: "uploading" })
    const props = getStatusBadgeProps(torrent, true, t)
    expect(props.variant).toBe("default")
    expect(props.className).toBe("")
    expect(props.iconClass).toBe("text-primary")
  })

  it("gives each tracker health its own color", () => {
    const classes = Object.keys(TRACKER_HEALTH_DISPLAY).map(health =>
      getStatusBadgeProps({ state: "uploading", tracker_health: health as TrackerHealth }, true, t).className
    )
    expect(new Set(classes).size).toBe(classes.length)
  })
})
