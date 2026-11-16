/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it } from "vitest"

import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"

afterEach(cleanup)

describe("ClientApiKeysManager", () => {
  it("lets the user create the first key when the server sends no list", async () => {
    // The server sends null, not an empty list, when no key exists.
    server.use(
      http.get("*/api/client-api-keys", () => HttpResponse.json(null)),
      http.get("*/api/instances", () => HttpResponse.json([]))
    )
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <ClientApiKeysManager />
        </TooltipProvider>
      </QueryClientProvider>
    )

    await screen.findByText("No keys yet")
    const create = screen.getByRole("button", { name: "Create key" }) as HTMLButtonElement
    expect(create.disabled).toBe(false)
  })
})
