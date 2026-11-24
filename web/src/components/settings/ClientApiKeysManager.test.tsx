/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import type { ChangeEvent, ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"

// jsdom cannot open the Radix select, so the test drives a native select.
vi.mock("@/components/ui/select", () => ({
  Select: ({ value, onValueChange, children }: { value: string; onValueChange: (value: string) => void; children: ReactNode }) => (
    <select aria-label="instance" value={value} onChange={(e: ChangeEvent<HTMLSelectElement>) => onValueChange(e.target.value)}>
      {children}
    </select>
  ),
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value }: { value: string }) => <option value={value}>{value}</option>,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))


afterEach(cleanup)

function renderManager() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ClientApiKeysManager />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe("ClientApiKeysManager", () => {
  // regression: #4233
  it("lets the user create the first key when the server sends no list", async () => {
    // The server sends null, not an empty list, when no key exists.
    server.use(
      http.get("*/api/client-api-keys", () => HttpResponse.json(null)),
      http.get("*/api/instances", () => HttpResponse.json([]))
    )
    renderManager()

    await screen.findByText("No keys yet")
    const create = screen.getByRole("button", { name: "Create key" }) as HTMLButtonElement
    expect(create.disabled).toBe(false)
  })

  // regression: #4233
  it("takes no second key until the user is done with the proxy URL of the first", async () => {
    const instance = { id: 1, name: "Seedbox", host: "http://127.0.0.1:8080" }
    const keys: unknown[] = []
    server.use(
      http.get("*/api/client-api-keys", () => HttpResponse.json(keys)),
      http.get("*/api/instances", () => HttpResponse.json([instance])),
      http.post("*/api/client-api-keys", async ({ request }) => {
        const { clientName } = await request.json() as { clientName: string }
        const clientApiKey = { id: keys.length + 1, clientName, instanceId: 1, createdAt: "2026-01-02T10:00:00Z" }
        keys.push({ ...clientApiKey, instance })
        return HttpResponse.json({ key: "key-a", clientApiKey, instance, proxyUrl: "/proxy/key-a" })
      })
    )
    renderManager()

    await screen.findByText("No keys yet")
    fireEvent.click(screen.getByRole("button", { name: "Create key" }))
    const editor = await screen.findByRole("dialog")
    fireEvent.change(within(editor).getByLabelText("Client name"), { target: { value: "Sonarr" } })
    fireEvent.change(await within(editor).findByRole("combobox", { name: "instance" }), { target: { value: "1" } })
    fireEvent.click(within(editor).getByRole("button", { name: "Create key" }))

    const notice = await screen.findByRole("status")
    expect(within(notice).getByLabelText("Proxy URL").textContent).toContain("/proxy/key-a")
    const create = screen.getByRole("button", { name: "Create key" }) as HTMLButtonElement
    expect(create.disabled).toBe(true)

    fireEvent.click(within(notice).getByRole("button", { name: "Done" }))
    expect(screen.queryByRole("status")).toBeNull()
    expect(create.disabled).toBe(false)
  })
})
