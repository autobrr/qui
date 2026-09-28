/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { RuleCondition } from "@/types"

// Return the key, plus the interpolated value when there is one. A stable function
// keeps renders from looping.
const mocks = vi.hoisted(() => ({
  t: (key: string, options?: { value?: string }) => (options?.value ? `${key}:${options.value}` : key),
}))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: mocks.t }),
}))

import { LeafCondition } from "./LeafCondition"

function renderCondition(condition: RuleCondition) {
  render(
    <TooltipProvider>
      <LeafCondition id="c1" condition={condition} onChange={vi.fn()} onRemove={vi.fn()} />
    </TooltipProvider>
  )
}

function comboboxTexts(): string[] {
  return screen.getAllByRole("combobox").map((el) => el.textContent ?? "")
}

describe("LeafCondition content type value", () => {
  afterEach(() => {
    cleanup()
  })

  it("selects the known type for a saved value in another case", () => {
    renderCondition({ field: "CONTENT_TYPE", operator: "EQUAL", value: "TV" })
    expect(comboboxTexts()).toContain("common:contentTypeLabels.tv")
    expect(screen.queryByRole("textbox")).toBeNull()
  })

  it("keeps an unknown saved value as a flagged option", () => {
    renderCondition({ field: "CONTENT_TYPE", operator: "NOT_EQUAL", value: "ebook" })
    expect(comboboxTexts()).toContain("queryBuilder.customContentType:ebook")
  })

  it("keeps free text for a condition saved with regex on", () => {
    renderCondition({ field: "CONTENT_TYPE", operator: "EQUAL", value: "^(movie|tv)$", regex: true })
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("^(movie|tv)$")
  })

  it("keeps free text for text operators", () => {
    renderCondition({ field: "CONTENT_TYPE", operator: "CONTAINS", value: "book" })
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("book")
  })
})
