/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"

import { SeedingLimitsForm } from "./SeedingLimitsForm"

const { hookResult, updatePreferences } = vi.hoisted(() => {
  const updatePreferences = vi.fn()
  return {
    updatePreferences,
    hookResult: {
      preferences: {
        max_ratio_enabled: true,
        max_ratio: 25,
        max_seeding_time_enabled: false,
        max_seeding_time: -1,
      },
      isLoading: false,
      updatePreferences,
      isUpdating: false,
    },
  }
})

vi.mock("@/hooks/useInstancePreferences", () => ({
  useInstancePreferences: () => hookResult,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

beforeEach(() => {
  // Radix tooltip measures through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  updatePreferences.mockClear()
})

function renderForm() {
  render(<TooltipProvider><SeedingLimitsForm instanceId={1} /></TooltipProvider>)
}

// The share ratio field is the first number input; the seeding time field follows it.
function ratioInput() {
  return screen.getAllByRole("spinbutton")[0] as HTMLInputElement
}

describe("SeedingLimitsForm share ratio", () => {
  it("keeps a stored ratio above 10 through an untouched save", async () => {
    renderForm()

    expect(ratioInput().value).toBe("25")
    // A native max attribute below the stored value makes the browser block the submit.
    const form = ratioInput().closest("form")!
    expect(form.checkValidity()).toBe(true)
    fireEvent.submit(form)

    await waitFor(() => expect(updatePreferences).toHaveBeenCalledWith(expect.objectContaining({ max_ratio: 25 })))
  })

  it("keeps a typed ratio above 10", () => {
    renderForm()

    fireEvent.change(ratioInput(), { target: { value: "150.5" } })

    expect(ratioInput().value).toBe("150.5")
  })

  it("stores -1 when the field is cleared", async () => {
    renderForm()

    fireEvent.change(ratioInput(), { target: { value: "" } })
    fireEvent.submit(ratioInput().closest("form")!)

    await waitFor(() => expect(updatePreferences).toHaveBeenCalledWith(expect.objectContaining({ max_ratio: -1 })))
  })
})
