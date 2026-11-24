/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import type { InstanceResponse } from "@/types"

const mocks = vi.hoisted(() => ({
  getInstances: vi.fn(),
  location: { pathname: "/dashboard" },
  navigate: () => {},
  search: {},
  unifiedFilter: [[], () => {}] as const,
  crossSeedState: { state: {} },
}))

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, params, children, ...props }: { to: string; params?: Record<string, string>; children: ReactNode }) => (
    <a href={to.replace("$instanceId", params?.instanceId ?? "")} {...props}>{children}</a>
  ),
  useLocation: () => mocks.location,
  useNavigate: () => mocks.navigate,
  useSearch: () => mocks.search,
}))
vi.mock("@/lib/api", () => ({ api: { getInstances: mocks.getInstances } }))
vi.mock("@/hooks/usePersistedUnifiedInstanceFilter", () => ({ usePersistedUnifiedInstanceFilter: () => mocks.unifiedFilter }))
vi.mock("@/hooks/useCrossSeedInstanceState", () => ({ useCrossSeedInstanceState: () => mocks.crossSeedState }))

import { MobileClientsTab } from "./MobileClientsTab"

function instance(id: number, name: string, isActive: boolean): InstanceResponse {
  return { id, name, isActive, connected: true } as InstanceResponse
}

function renderTab(instances: InstanceResponse[]) {
  mocks.getInstances.mockResolvedValue(instances)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MobileClientsTab />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("MobileClientsTab", () => {
  // regression: #4359
  it("opens the only enabled instance directly, with no menu", async () => {
    const { container } = renderTab([instance(1, "main", true), instance(2, "spare", false)])

    const tab = await screen.findByRole("link", { name: /Clients/ })
    expect(tab.getAttribute("href")).toBe("/instances/1")
    expect(container.querySelector("[aria-haspopup]")).toBeNull()
  })

  it("opens a menu when more than one instance is enabled", async () => {
    renderTab([instance(1, "main", true), instance(2, "spare", true)])

    const tab = await screen.findByRole("button", { name: /Clients/ })
    expect(tab.getAttribute("aria-haspopup")).toBe("menu")
    expect(screen.queryByRole("link")).toBeNull()
  })
})
