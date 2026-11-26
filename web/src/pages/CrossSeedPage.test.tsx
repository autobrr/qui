/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: () => undefined }))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve({ enabled: false }),
    getInstances: () => new Promise(() => {}),
    listTorznabIndexers: () => new Promise(() => {}),
  },
}))
vi.mock("@/components/cross-seed/RssTab", () => ({ RssTab: () => <div data-testid="tab-rss" /> }))
vi.mock("@/components/cross-seed/WebhookTab", () => ({ WebhookTab: () => <div data-testid="tab-webhook" /> }))
vi.mock("@/components/cross-seed/CompletionTab", () => ({ CompletionTab: () => <div data-testid="tab-completion" /> }))
vi.mock("@/components/cross-seed/LibraryTab", () => ({ LibraryTab: () => <div data-testid="tab-library" /> }))
vi.mock("@/components/cross-seed/DirScanTab", () => ({ DirScanTab: () => <div data-testid="tab-directories" /> }))
vi.mock("@/components/cross-seed/SeasonPacksTab", () => ({ SeasonPacksTab: () => <div data-testid="tab-season-packs" /> }))
vi.mock("@/components/cross-seed/MatchingTab", () => ({ MatchingTab: () => <div data-testid="tab-matching" /> }))
vi.mock("@/components/cross-seed/CategoriesTab", () => ({ CategoriesTab: () => <div data-testid="tab-categories" /> }))
vi.mock("@/components/cross-seed/AfterInjectionTab", () => ({ AfterInjectionTab: () => <div data-testid="tab-after-injection" /> }))
vi.mock("@/components/cross-seed/BlocklistTab", () => ({ BlocklistTab: () => <div data-testid="tab-blocklist" /> }))

import { CROSS_SEED_TABS } from "@/components/cross-seed/tabs"
import { CrossSeedPage } from "./CrossSeedPage"

afterEach(cleanup)

describe("CrossSeedPage tabs", () => {
  it.each(CROSS_SEED_TABS)("renders only the %s tab when it is active", async (tab) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <CrossSeedPage activeTab={tab} onTabChange={() => {}} />
      </QueryClientProvider>
    )

    expect(await screen.findByTestId(`tab-${tab}`)).toBeTruthy()
    for (const other of CROSS_SEED_TABS) {
      if (other !== tab) expect(screen.queryByTestId(`tab-${other}`)).toBeNull()
    }
  })
})
