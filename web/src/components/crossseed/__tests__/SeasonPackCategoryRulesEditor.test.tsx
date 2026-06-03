/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen, fireEvent } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

// Return the key itself so assertions can target stable strings without loading
// the full i18n runtime.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { SeasonPackCategoryRulesEditor } from "@/components/crossseed/SeasonPackCategoryRulesEditor"
import type { SeasonPackCategoryRule } from "@/types"

describe("SeasonPackCategoryRulesEditor", () => {
  // globals: false in vitest.config disables auto-cleanup, so do it explicitly.
  afterEach(() => {
    cleanup()
  })

  it("appends a default rule when Add rule is clicked", () => {
    const onChange = vi.fn()
    render(
      <SeasonPackCategoryRulesEditor
        value={[]}
        onChange={onChange}
        categoryMetadata={{}}
      />
    )

    fireEvent.click(screen.getByText("rules.seasonPack.categoryRouting.addRule"))

    expect(onChange).toHaveBeenCalledWith([
      { resolution: "1080p", source: "", category: "" },
    ])
  })

  it("removes the targeted rule", () => {
    const onChange = vi.fn()
    const value: SeasonPackCategoryRule[] = [
      { resolution: "2160p", source: "WEB", category: "tv-uhd" },
      { resolution: "1080p", source: "", category: "tv-hd" },
    ]
    render(
      <SeasonPackCategoryRulesEditor
        value={value}
        onChange={onChange}
        categoryMetadata={{}}
      />
    )

    const removeButtons = screen.getAllByLabelText("rules.seasonPack.categoryRouting.removeRule")
    fireEvent.click(removeButtons[0])

    expect(onChange).toHaveBeenCalledWith([
      { resolution: "1080p", source: "", category: "tv-hd" },
    ])
  })

  it("disables the Add rule button when disabled", () => {
    render(
      <SeasonPackCategoryRulesEditor
        value={[]}
        onChange={vi.fn()}
        categoryMetadata={{}}
        disabled
      />
    )

    const addButton = screen.getByText("rules.seasonPack.categoryRouting.addRule").closest("button")
    expect(addButton).not.toBeNull()
    expect((addButton as HTMLButtonElement).disabled).toBe(true)
  })
})
