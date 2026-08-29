/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  const text: Record<string, string> = {
    "rules.postInjection.pooledPartialCompletion": "Automatically pool torrents with extra data",
    "rules.postInjection.pooledPartialCompletionDescription": "Uses the existing maximum auto-start download value, and coordinates one downloader per related cross-seed pool for handling extra files in cross-seeds.",
  }
  return {
    ...actual,
    useTranslation: () => ({ t: (key: string) => text[key] ?? key }),
  }
})

vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))

import { buildPooledCompletionPatch } from "@/lib/crossseed-settings"
import { PooledCompletionSetting } from "@/pages/CrossSeedPage"

afterEach(cleanup)

describe("PooledCompletionSetting", () => {
  it("loads, toggles, and shows help", () => {
    const onCheckedChange = vi.fn()
    render(
      <PooledCompletionSetting
        id="pooled-partial-completion"
        checked
        onCheckedChange={onCheckedChange}
      />
    )

    const checkbox = screen.getByRole("checkbox", { name: "Automatically pool torrents with extra data" })
    expect(checkbox.getAttribute("aria-checked")).toBe("true")
    expect(screen.getByText(/one downloader per related cross-seed pool/)).toBeTruthy()

    fireEvent.click(checkbox)
    expect(onCheckedChange).toHaveBeenCalledWith(false)
  })

  it("builds the save payload with both bound values", () => {
    expect(buildPooledCompletionPatch(true, 125)).toEqual({
      pooledPartialCompletionEnabled: true,
      autoResumeMaxDownloadMb: 125,
    })
  })
})
