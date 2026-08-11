// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  const text: Record<string, string> = {
    "rules.safety.rescueTitleMismatches": "Rescue title mismatches",
    "rules.safety.rescueTitleMismatchesDescription": "Try up to three exact-size results per source search, total across all indexers. qui checks all data before it starts the torrent. Skip recheck turns this off.",
  }
  return {
    ...actual,
    useTranslation: () => ({ t: (key: string) => text[key] ?? key }),
  }
})

import { TitleRescueSetting } from "@/pages/CrossSeedPage"

afterEach(cleanup)

describe("TitleRescueSetting", () => {
  it("shows the saved value and reports changes", () => {
    const onCheckedChange = vi.fn()
    render(<TitleRescueSetting checked onCheckedChange={onCheckedChange} />)

    const toggle = screen.getByRole("switch", { name: "Rescue title mismatches" })
    expect(toggle.getAttribute("aria-checked")).toBe("true")
    expect(screen.getByText("Try up to three exact-size results per source search, total across all indexers. qui checks all data before it starts the torrent. Skip recheck turns this off.")).toBeTruthy()

    fireEvent.click(toggle)
    expect(onCheckedChange).toHaveBeenCalledWith(false)
  })

  it("does not allow changes when Skip recheck is on", () => {
    const onCheckedChange = vi.fn()
    render(<TitleRescueSetting checked={false} disabled onCheckedChange={onCheckedChange} />)

    const toggle = screen.getByRole("switch", { name: "Rescue title mismatches" })
    expect(toggle.hasAttribute("disabled")).toBe(true)
    fireEvent.click(toggle)
    expect(onCheckedChange).not.toHaveBeenCalled()
  })
})
