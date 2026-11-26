/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const translation = vi.hoisted(() => ({ t: (key: string) => key }))
const instances = vi.hoisted(() => ({ instances: [] }))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => translation,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))
vi.mock("@/components/instances/preferences/WorkflowsOverview", () => ({ WorkflowsOverview: () => <div data-testid="section-workflows" /> }))
vi.mock("@/components/instances/preferences/ReannounceOverview", () => ({ ReannounceOverview: () => <div data-testid="section-reannounce" /> }))
vi.mock("@/components/instances/preferences/OrphanScanOverview", () => ({ OrphanScanOverview: () => <div data-testid="section-orphan-scan" /> }))
vi.mock("@/components/instances/preferences/ReannounceSettingsDialog", () => ({ ReannounceSettingsDialog: () => null }))
vi.mock("@/components/instances/preferences/OrphanScanSettingsDialog", () => ({ OrphanScanSettingsDialog: () => null }))

import { AUTOMATIONS_TABS } from "@/components/automations/tabs"
import { Automations } from "./Automations"

afterEach(cleanup)

describe("Automations sections", () => {
  it.each(AUTOMATIONS_TABS)("renders only the %s section when it is active", (tab) => {
    render(<Automations activeTab={tab} onTabChange={() => {}} />)

    expect(screen.getByTestId(`section-${tab}`)).toBeTruthy()
    for (const other of AUTOMATIONS_TABS) {
      if (other !== tab) expect(screen.queryByTestId(`section-${other}`)).toBeNull()
    }
  })
})
