/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const activation = vi.hoisted(() => ({
  reset: vi.fn(),
  isPending: false,
  isError: false,
  error: null as Error | null,
}))

const licenses = vi.hoisted(() => ({
  data: [{ licenseKey: "TEST-KEY-0000", status: "invalid", productName: "premium" }],
}))
const invalidLicenses = licenses.data
const deleteLicense = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }))
const premiumAccess = vi.hoisted(() => ({ hasPremiumAccess: false, isSupporter: false, isLoading: false }))
const formatters = vi.hoisted(() => ({ formatDate: () => "" }))

vi.mock("@/hooks/useLicense", () => ({
  useActivateLicense: () => activation,
  useDeleteLicense: () => deleteLicense,
  useHasPremiumAccess: () => premiumAccess,
  useLicenseDetails: () => licenses,
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => formatters,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: { status?: string }) => (options?.status ? `${key} ${options.status}` : key),
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
}))

import { LicenseManager } from "@/components/themes/LicenseManager"
import { TooltipProvider } from "@/components/ui/tooltip"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  activation.isPending = false
  activation.isError = false
  licenses.data = invalidLicenses
  premiumAccess.hasPremiumAccess = false
  premiumAccess.isSupporter = false
})

describe("LicenseManager", () => {
  it("keeps a pending activation when the Add License dialog opens", () => {
    activation.isPending = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    fireEvent.click(screen.getByText("themes.license.actions.addLicense"))

    expect(activation.reset).not.toHaveBeenCalled()
  })

  it("clears a failed activation when the Add License dialog opens", () => {
    activation.isError = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    fireEvent.click(screen.getByText("themes.license.actions.addLicense"))

    expect(activation.reset).toHaveBeenCalledOnce()
  })

  it("explains an offline license and offers no re-activation", () => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status: "offline", productName: "premium" }]
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.offlineTitle")).not.toBeNull()
    expect(screen.queryByText("themes.license.status.activationRequiredTitle")).toBeNull()
    expect(screen.queryByText("themes.license.actions.reactivate")).toBeNull()
    expect(screen.queryByText("themes.license.actions.addLicense")).toBeNull()
    expect(screen.queryByText("themes.license.actions.getPremium")).toBeNull()
  })

  it("lets a supporter add the theme license, and does not show the supporter key as one", () => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status: "active", productName: "supporter" }]
    premiumAccess.hasPremiumAccess = true
    premiumAccess.isSupporter = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.supporterTitle")).not.toBeNull()
    expect(screen.getByText("themes.license.status.supporterDescription")).not.toBeNull()
    expect(screen.getByText("themes.license.actions.addLicense")).not.toBeNull()
    expect(screen.queryByText("themes.license.actions.remove")).toBeNull()
  })

  it("says that the support unlocks the themes when the stored theme license is not active", () => {
    licenses.data = [
      { licenseKey: "TEST-KEY-0000", status: "invalid", productName: "premium" },
      { licenseKey: "TEST-KEY-0001", status: "active", productName: "supporter" },
    ]
    premiumAccess.hasPremiumAccess = true
    premiumAccess.isSupporter = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.supporterTitle")).not.toBeNull()
    expect(screen.getByText("themes.license.status.supporterDescription")).not.toBeNull()
    expect(screen.queryByText("themes.license.status.activeTitle")).toBeNull()
  })

  it("says that the license is active for a supporter with an active theme license", () => {
    licenses.data = [
      { licenseKey: "TEST-KEY-0000", status: "active", productName: "premium" },
      { licenseKey: "TEST-KEY-0001", status: "active", productName: "supporter" },
    ]
    premiumAccess.hasPremiumAccess = true
    premiumAccess.isSupporter = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.activeTitle")).not.toBeNull()
    expect(screen.getByText("themes.license.status.activeDescription")).not.toBeNull()
  })

  it("credits an older theme license, not support that ended, when the newest theme license is offline", () => {
    licenses.data = [
      { licenseKey: "TEST-KEY-0000", status: "offline", productName: "premium" },
      { licenseKey: "TEST-KEY-0001", status: "invalid", productName: "supporter" },
    ]
    premiumAccess.hasPremiumAccess = true
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.activeTitle")).not.toBeNull()
    expect(screen.queryByText("themes.license.status.supporterTitle")).toBeNull()
  })

  // regression: #4276
  it("explains a supporter key that is offline, and offers no purchase", () => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status: "offline", productName: "supporter" }]
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.offlineTitle")).not.toBeNull()
    expect(screen.queryByText("themes.license.status.supportEndedDescription")).toBeNull()
    expect(screen.queryByText("themes.license.actions.getPremium")).toBeNull()
  })

  it("tells a past supporter where the theme license key is", () => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status: "invalid", productName: "supporter" }]
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText("themes.license.status.supportEndedDescription")).not.toBeNull()
    expect(screen.queryByText("themes.license.actions.reactivate")).toBeNull()
    expect(screen.getByText("themes.license.actions.getPremium")).not.toBeNull()
  })

  it.each([
    ["invalid", "themes.license.details.statusLabels.invalid"],
    ["offline", "themes.license.details.statusLabels.offline"],
    ["expired", "themes.license.details.statusLabels.unknown"],
  ])("shows a translated label for the %s status", (status, labelKey) => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status, productName: "premium" }]
    render(<LicenseManager />, { wrapper: TooltipProvider })

    expect(screen.getByText(`themes.license.details.productStatusAndAdded ${labelKey}`)).not.toBeNull()
  })
})
