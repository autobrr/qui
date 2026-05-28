/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getStateLabel } from "@/lib/torrent-state-utils"
import type { TFunction } from "i18next"
import { describe, expect, it, vi } from "vitest"

describe("getStateLabel", () => {
  it("returns the English fallback when no translator is provided", () => {
    expect(getStateLabel("uploading")).toBe("Seeding")
    expect(getStateLabel("stalledUP")).toBe("Seeding")
    expect(getStateLabel("stoppedUP")).toBe("Completed")
    expect(getStateLabel("stoppedDL")).toBe("Stopped")
  })

  it("passes through unknown states unchanged", () => {
    expect(getStateLabel("someFutureState")).toBe("someFutureState")
  })

  it("translates known states through the i18n key with the English fallback as defaultValue", () => {
    const t = vi.fn(() => "做种中") as unknown as TFunction

    expect(getStateLabel("uploading", t)).toBe("做种中")
    expect(t).toHaveBeenCalledWith("stateLabels.uploading", { defaultValue: "Seeding" })
  })

  it("uses the key derived from the raw qBittorrent state, not the friendly label", () => {
    const t = vi.fn((_key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? "") as unknown as TFunction

    getStateLabel("stalledUP", t)
    expect(t).toHaveBeenCalledWith("stateLabels.stalledUP", { defaultValue: "Seeding" })

    getStateLabel("forcedDL", t)
    expect(t).toHaveBeenCalledWith("stateLabels.forcedDL", { defaultValue: "(F) Downloading" })
  })

  it("falls back to the defaultValue for unknown states when a translator is provided", () => {
    const t = vi.fn((_key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? "") as unknown as TFunction

    expect(getStateLabel("someFutureState", t)).toBe("someFutureState")
    expect(t).toHaveBeenCalledWith("stateLabels.someFutureState", { defaultValue: "someFutureState" })
  })
})
