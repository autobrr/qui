/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const translation = vi.hoisted(() => ({ t: (key: string) => key }))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => translation,
}))
vi.mock("@/components/transfers/TransfersTab", () => ({ TransfersTab: () => <div data-testid="section-transfers" /> }))
vi.mock("@/components/shares/SharesTab", () => ({ SharesTab: () => <div data-testid="section-shares" /> }))

import { TRANSFERS_TABS } from "@/components/transfers/tabs"
import { TransfersPage } from "./Transfers"

afterEach(cleanup)

describe("Transfers sections", () => {
  it.each(TRANSFERS_TABS)("renders only the %s section when it is active", (tab) => {
    render(<TransfersPage activeTab={tab} onTabChange={() => {}} />)

    expect(screen.getByTestId(`section-${tab}`)).toBeTruthy()
    for (const other of TRANSFERS_TABS) {
      if (other !== tab) expect(screen.queryByTestId(`section-${other}`)).toBeNull()
    }
  })
})
