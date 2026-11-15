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
const premiumAccess = vi.hoisted(() => ({ hasPremiumAccess: false, isLoading: false }))
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

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  activation.isPending = false
  activation.isError = false
  licenses.data = invalidLicenses
})

describe("LicenseManager", () => {
  it("keeps a pending activation when the Add License dialog opens", () => {
    activation.isPending = true
    render(<LicenseManager />)

    fireEvent.click(screen.getByText("themes.license.actions.addLicense"))

    expect(activation.reset).not.toHaveBeenCalled()
  })

  it("clears a failed activation when the Add License dialog opens", () => {
    activation.isError = true
    render(<LicenseManager />)

    fireEvent.click(screen.getByText("themes.license.actions.addLicense"))

    expect(activation.reset).toHaveBeenCalledOnce()
  })

  it("explains an offline license and offers no re-activation", () => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status: "offline", productName: "premium" }]
    render(<LicenseManager />)

    expect(screen.getByText("themes.license.status.offlineTitle")).not.toBeNull()
    expect(screen.queryByText("themes.license.status.activationRequiredTitle")).toBeNull()
    expect(screen.queryByText("themes.license.actions.reactivate")).toBeNull()
    expect(screen.queryByText("themes.license.actions.addLicense")).toBeNull()
    expect(screen.queryByText("themes.license.actions.getPremium")).toBeNull()
  })

  it.each([
    ["invalid", "themes.license.details.statusLabels.invalid"],
    ["offline", "themes.license.details.statusLabels.offline"],
    ["expired", "themes.license.details.statusLabels.unknown"],
  ])("shows a translated label for the %s status", (status, labelKey) => {
    licenses.data = [{ licenseKey: "TEST-KEY-0000", status, productName: "premium" }]
    render(<LicenseManager />)

    expect(screen.getByText(`themes.license.details.productStatusAndAdded ${labelKey}`)).not.toBeNull()
  })
})
