/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { InstanceResponse } from "@/types"

import { InstanceList } from "./InstanceList"

const instances = [
  { id: 1, name: "seedbox", host: "http://127.0.0.1:8080", isActive: true, connected: true },
  { id: 2, name: "library", host: "http://127.0.0.1:8081", isActive: true, connected: true },
] as InstanceResponse[]

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

// regression: #4233
it("keeps the switch of an instance disabled until its own status save ends", async () => {
  const answers = new Map<string, () => void>()
  server.use(
    http.get("*/api/instances", () => HttpResponse.json(instances)),
    http.put("*/api/instances/:id/status", async ({ params }) => {
      await new Promise<void>((resolve) => answers.set(String(params.id), resolve))
      return HttpResponse.json({ ...instances.find((instance) => String(instance.id) === params.id), isActive: false })
    })
  )
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <InstanceList instances={instances} onEdit={() => {}} onChangeIcon={() => {}} onAdd={() => {}} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const switchOf = (name: string) => within(screen.getByRole("listitem", { name })).getByRole<HTMLButtonElement>("switch")

  fireEvent.click(switchOf("seedbox"))
  await waitFor(() => expect(answers.has("1")).toBe(true))
  fireEvent.click(switchOf("library"))
  await waitFor(() => expect(answers.has("2")).toBe(true))

  expect(switchOf("seedbox").disabled).toBe(true)
  expect(switchOf("library").disabled).toBe(true)

  answers.get("1")?.()
  await waitFor(() => expect(switchOf("seedbox").disabled).toBe(false))
  expect(switchOf("library").disabled).toBe(true)
  answers.get("2")?.()
})
