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
    "rules.postInjection.pooledPartialCompletion": "Automatically complete partial hardlink/reflink cross-seeds",
    "rules.postInjection.pooledPartialCompletionDescription": "Uses the existing maximum auto-start download value and coordinates one downloader per related pool. Applies only to hardlink and reflink modes.",
    "rules.postInjection.maxAutoResumeDownload": "Max auto-start download (MiB)",
    "rules.postInjection.maxAutoResumeDownloadDescription": "After recheck, this also gates pooled acquisition.",
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
  it("loads, toggles, shows help, and keeps the existing byte budget bound", () => {
    const onCheckedChange = vi.fn()
    const onBudgetChange = vi.fn()
    render(
      <PooledCompletionSetting
        checked
        autoResumeMaxDownloadMb={75}
        onCheckedChange={onCheckedChange}
        onAutoResumeMaxDownloadMbChange={onBudgetChange}
      />
    )

    const toggle = screen.getByRole("switch", { name: "Automatically complete partial hardlink/reflink cross-seeds" })
    expect(toggle.getAttribute("aria-checked")).toBe("true")
    expect(screen.getByText(/one downloader per related pool/)).toBeTruthy()
    expect(screen.getByText(/also gates pooled acquisition/)).toBeTruthy()

    fireEvent.click(toggle)
    expect(onCheckedChange).toHaveBeenCalledWith(false)

    const budget = screen.getByRole("spinbutton", { name: "Max auto-start download (MiB)" })
    expect((budget as HTMLInputElement).value).toBe("75")
    fireEvent.change(budget, { target: { value: "125" } })
    expect(onBudgetChange).toHaveBeenCalledWith(125)
  })

  it("builds the save payload with both bound values", () => {
    expect(buildPooledCompletionPatch(true, 125)).toEqual({
      pooledPartialCompletionEnabled: true,
      autoResumeMaxDownloadMb: 125,
    })
  })
})
