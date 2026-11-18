/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { SupportButton } from "./SupportButton"

const view = vi.hoisted(() => ({ wide: true }))
const reminder = vi.hoisted(() => ({ due: false, days: 412, shortcutHidden: false }))

vi.mock("@/hooks/useMediaQuery", () => ({ useMediaQuery: () => view.wide }))
vi.mock("@/hooks/useLicense", () => ({ useHasPremiumAccess: () => ({ isSupporter: false }) }))
vi.mock("./reminder", () => ({ notNow: vi.fn(), useSupportReminder: () => reminder }))
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(() => {
  cleanup()
  view.wide = true
  reminder.due = false
  reminder.shortcutHidden = false
})

const renderButton = () => render(<TooltipProvider><SupportButton /></TooltipProvider>)

describe("SupportButton", () => {
  it("shows nothing while the shortcut is hidden, also when the reminder is due", () => {
    reminder.shortcutHidden = true
    reminder.due = true
    const { container } = renderButton()

    expect(container.childElementCount).toBe(0)
  })

  it("grows into the reminder pill on a wide header", () => {
    reminder.due = true
    renderButton()

    expect(screen.getByLabelText("support.reminder.notNow")).not.toBeNull()
  })

  it("stays a heart on a narrow header, where the row above the page has the reminder", () => {
    reminder.due = true
    view.wide = false
    renderButton()

    expect(screen.getByLabelText("support.title")).not.toBeNull()
    expect(screen.queryByLabelText("support.reminder.notNow")).toBeNull()
  })
})
