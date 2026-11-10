/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const stubs = vi.hoisted(() => {
  const pending = new Promise<never>(() => {})
  const call = () => pending
  return {
    translation: { t: (key: string) => key, i18n: { language: "en" } },
    api: new Proxy({}, { get: () => call }),
    selfUpdate: { setConfirmOpen: () => {} },
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => stubs.translation,
}))
vi.mock("@/lib/api", () => ({
  api: stubs.api,
  APIError: class extends Error {},
}))
vi.mock("@/hooks/useSelfUpdate", () => ({ useSelfUpdate: () => stubs.selfUpdate }))
vi.mock("@/components/indexers/IndexersPage", () => ({ IndexersPage: () => <div data-testid="section-indexers" /> }))
vi.mock("@/components/settings/GazelleSettingsCard", () => ({ GazelleSettingsCard: () => null }))
vi.mock("@/components/settings/ArrInstancesManager", () => ({ ArrInstancesManager: () => <div data-testid="section-integrations" /> }))
vi.mock("@/components/settings/ClientApiKeysManager", () => ({ ClientApiKeysManager: () => <div data-testid="section-client-api" /> }))
vi.mock("@/components/settings/ExternalProgramsManager", () => ({ ExternalProgramsManager: () => <div data-testid="section-external-programs" /> }))
vi.mock("@/components/settings/NotificationsManager", () => ({ NotificationsManager: () => <div data-testid="section-notifications" /> }))
vi.mock("@/components/settings/DateTimePreferencesForm", () => ({ DateTimePreferencesForm: () => <div data-testid="section-datetime" /> }))
vi.mock("@/components/settings/LogSettingsPanel", () => ({ LogSettingsPanel: () => <div data-testid="section-logs" /> }))
vi.mock("@/components/themes/ThemeSelector", () => ({ ThemeSelector: () => <div data-testid="section-themes" /> }))
vi.mock("@/components/themes/LicenseManager.tsx", () => ({ LicenseManager: () => null }))

import { SETTINGS_TABS, type SettingsTab } from "@/components/settings/tabs"
import { Settings } from "./Settings"

// Sections that Settings.tsx renders inline show a translation key that only they render.
const INLINE_MARKERS: Partial<Record<SettingsTab, string>> = {
  application: "application.loading",
  instances: "instances.title",
  "search-cache": "searchCache.disabled",
  api: "apiKeys.cardTitle",
  security: "security.changePassword.title",
}

function isShown(tab: SettingsTab) {
  const marker = INLINE_MARKERS[tab]
  return marker ? screen.queryAllByText(marker).length > 0 : screen.queryByTestId(`section-${tab}`) !== null
}

afterEach(cleanup)

describe("Settings sections", () => {
  it.each(SETTINGS_TABS)("renders only the %s section when it is active", (tab) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <Settings search={{ tab }} onSearchChange={() => {}} />
      </QueryClientProvider>
    )

    expect(isShown(tab)).toBe(true)
    for (const other of SETTINGS_TABS) {
      if (other !== tab) expect(isShown(other), other).toBe(false)
    }
  })
})
