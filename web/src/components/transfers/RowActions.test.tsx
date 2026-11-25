/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { Download } from "lucide-react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"

import { RowActions, type RowAction } from "./RowActions"

const screenSize = vi.hoisted(() => ({ phone: false }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsPhone: () => screenSize.phone }))

afterEach(() => {
  screenSize.phone = false
  cleanup()
})

function renderActions(action: RowAction) {
  render(
    <TooltipProvider>
      <span id="row-name">Row</span>
      <RowActions actions={[action]} describedBy="row-name" />
    </TooltipProvider>
  )
}

describe("RowActions", () => {
  it("downloads the file of an action with a link", () => {
    renderActions({ label: ".torrent", icon: Download, href: "/api/shares/41/torrent" })
    expect(screen.getByRole("link", { name: ".torrent" }).getAttribute("href")).toBe("/api/shares/41/torrent")
  })

  it.each([
    ["a wide screen", { pending: true }, false],
    ["a wide screen", { disabled: true }, false],
    ["a phone", { pending: true }, true],
    ["a phone", { disabled: true }, true],
  ])("offers no download link on %s while the action takes no press: %o", (_screen, flags, phone) => {
    screenSize.phone = phone
    renderActions({ label: ".torrent", icon: Download, href: "/api/shares/41/torrent", ...flags })
    expect(screen.queryByRole("link")).toBeNull()
    expect(screen.getByRole<HTMLButtonElement>("button", { name: ".torrent" }).disabled).toBe(true)
  })
})
