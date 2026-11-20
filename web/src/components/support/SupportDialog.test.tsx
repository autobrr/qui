/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const access = vi.hoisted(() => ({ hasPremiumAccess: false, isSupporter: false }))
const licenses = vi.hoisted(() => ({
  data: [] as Array<{ licenseKey: string; status: string; productName: string }> | undefined,
}))
const activation = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }))
const deletion = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }))

vi.mock("@/hooks/useLicense", () => ({
  useActivateLicense: () => activation,
  useDeleteLicense: () => deletion,
  useHasPremiumAccess: () => access,
  useLicenseDetails: () => licenses,
}))

vi.mock("@/hooks/useMediaQuery", () => ({ useIsPhone: () => false }))

const reminder = vi.hoisted(() => ({ due: false, days: 0, settled: true, isSupporter: false, shortcutHidden: false, reminderOff: false }))
const answerReminder = vi.hoisted(() => vi.fn())
vi.mock("@/components/support/reminder", () => ({
  answerReminder,
  hideShortcut: vi.fn(),
  turnOffReminder: vi.fn(),
  useSupportReminder: () => reminder,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: { price?: string; count?: number }) =>
      options?.price ? `${key} ${options.price}` : options?.count !== undefined ? `${key} ${options.count}` : key,
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
}))

import { SupportDialog } from "@/components/support/SupportDialog"
import { DODO_CUSTOMER_PORTAL_URL, DODO_SUPPORTER_PLANS } from "@/lib/dodo-constants"

const checkoutHref = () => screen.getByText("support.checkout").closest("a")?.getAttribute("href")

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  access.hasPremiumAccess = false
  access.isSupporter = false
  licenses.data = []
  activation.isPending = false
  reminder.due = false
})

describe("SupportDialog", () => {
  it("sends each plan to its own checkout", () => {
    render(<SupportDialog open onOpenChange={() => {}} />)

    expect(checkoutHref()).toBe(DODO_SUPPORTER_PLANS[0].url)

    fireEvent.click(screen.getByText(`support.plans.yearly ${DODO_SUPPORTER_PLANS[1].price}`))
    expect(checkoutHref()).toBe(DODO_SUPPORTER_PLANS[1].url)

    fireEvent.click(screen.getByText("support.plans.license $9.99"))
    expect(checkoutHref()).toContain("redirect_url=")
  })

  it("does not offer the theme license to a server that has it", () => {
    access.hasPremiumAccess = true
    render(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.getByText("support.licensed")).not.toBeNull()
    expect(screen.queryByText("support.plans.license $9.99")).toBeNull()
    expect(checkoutHref()).toBe(DODO_SUPPORTER_PLANS[0].url)
  })

  it("does not offer the theme license to a server whose stored license is not valid", () => {
    licenses.data = [{ licenseKey: "KEY-1", status: "invalid", productName: "premium-access" }]
    render(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.queryByText("support.plans.license $9.99")).toBeNull()
  })

  // regression: #4276
  it("keeps the theme license checkout closed until the license list loads", () => {
    licenses.data = undefined
    const { rerender } = render(<SupportDialog open onOpenChange={() => {}} />)
    fireEvent.click(screen.getByText("support.plans.license $9.99"))

    expect(checkoutHref()).toBeNull()

    licenses.data = []
    rerender(<SupportDialog open onOpenChange={() => {}} />)

    expect(checkoutHref()).toContain("redirect_url=")
  })

  it("goes back to a yearly plan when the picked theme license goes away", () => {
    const { rerender } = render(<SupportDialog open onOpenChange={() => {}} />)
    fireEvent.click(screen.getByText("support.plans.license $9.99"))

    // The license list loads with a stored license.
    licenses.data = [{ licenseKey: "KEY-1", status: "invalid", productName: "premium-access" }]
    rerender(<SupportDialog open onOpenChange={() => {}} />)

    expect(checkoutHref()).toBe(DODO_SUPPORTER_PLANS[0].url)
  })

  it("asks for the supporter key after the checkout opens, and activates the key", () => {
    const { rerender } = render(<SupportDialog open onOpenChange={() => {}} />)

    fireEvent.click(screen.getByText("support.checkout"))
    fireEvent.change(screen.getByLabelText("support.key.label"), { target: { value: "  KEY-1  " } })
    fireEvent.click(screen.getByText("support.key.submit"))

    expect(activation.mutate.mock.calls[0][0]).toBe("KEY-1")

    // regression: #4276
    activation.isPending = true
    rerender(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.getByText("support.key.back").closest("button")?.disabled).toBe(true)
  })

  it("thanks a supporter, and offers no plan", () => {
    access.hasPremiumAccess = true
    access.isSupporter = true
    licenses.data = [{ licenseKey: "KEY-1", status: "active", productName: "supporter" }]
    render(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.getByText("support.supporter.manage").closest("a")?.getAttribute("href")).toBe(DODO_CUSTOMER_PORTAL_URL)
    expect(screen.queryByText("support.checkout")).toBeNull()

    fireEvent.click(screen.getByText("support.removeKey"))
    expect(deletion.mutate).toHaveBeenCalledWith("KEY-1")
  })

  it("lets a supporter with a key that is not valid fix the payment or remove the key", () => {
    licenses.data = [{ licenseKey: "KEY-1", status: "invalid", productName: "supporter" }]
    render(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.getByText("support.checkout")).not.toBeNull()
    expect(screen.getByText("support.supporter.manage").closest("a")?.getAttribute("href")).toBe(DODO_CUSTOMER_PORTAL_URL)
    fireEvent.click(screen.getByText("support.removeKey"))
    expect(deletion.mutate).toHaveBeenCalledWith("KEY-1")
  })

  it("lets the user remove a supporter key that is offline", () => {
    licenses.data = [{ licenseKey: "KEY-1", status: "offline", productName: "supporter" }]
    render(<SupportDialog open onOpenChange={() => {}} />)

    fireEvent.click(screen.getByText("support.removeKey"))
    expect(deletion.mutate).toHaveBeenCalledWith("KEY-1")
  })

  it("answers a due reminder on open, and keeps the day count for that open", () => {
    reminder.due = true
    reminder.days = 412
    const { rerender } = render(<SupportDialog open onOpenChange={() => {}} />)

    expect(answerReminder).toHaveBeenCalledTimes(1)

    // The answer ends the reminder.
    reminder.due = false
    rerender(<SupportDialog open onOpenChange={() => {}} />)

    expect(screen.getByText("support.reminder.serverDays 412")).not.toBeNull()
  })
})
