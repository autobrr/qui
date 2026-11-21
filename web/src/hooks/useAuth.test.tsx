/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useAuth } from "./useAuth"

const mocks = vi.hoisted(() => ({
  setup: vi.fn(),
  checkAuth: vi.fn(),
  navigate: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  api: {
    setup: mocks.setup,
    checkAuth: mocks.checkAuth,
    login: vi.fn(),
    logout: vi.fn(),
  },
}))

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-router")>(),
  useNavigate: () => mocks.navigate,
}))

let queryClient: QueryClient

beforeEach(() => {
  mocks.checkAuth.mockRejectedValue(new Error("signed out"))
  mocks.setup.mockResolvedValue({ user: { id: 1, username: "owner" } })
  sessionStorage.clear()
})

afterEach(() => {
  cleanup()
  queryClient?.clear()
  vi.clearAllMocks()
})

describe("useAuth setup", () => {
  // regression: #4313
  it("clears both setup-required caches after the first account exists", async () => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    queryClient.setQueryData(["setup-required"], true)
    queryClient.setQueryData(["setup-required-page"], true)
    const initialClient = queryClient

    const { result } = renderHook(() => useAuth(), {
      wrapper: ({ children }) => (
        <QueryClientProvider client={initialClient}>{children}</QueryClientProvider>
      ),
    })

    result.current.setup({ username: "owner", password: "password123" })

    await waitFor(() => expect(initialClient.getQueryData(["setup-required"])).toBe(false))
    expect(initialClient.getQueryData(["setup-required-page"])).toBe(false)
  })
})
