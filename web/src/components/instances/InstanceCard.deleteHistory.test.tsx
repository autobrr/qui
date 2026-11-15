/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { InstanceResponse } from "@/types"

import { InstanceCard } from "./InstanceCard"

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))

const instance = { id: 1, name: "seedbox", host: "http://127.0.0.1:8080", isActive: true, connected: true } as InstanceResponse

let requests: string[]
let historyStatus: number
let historyAnswer: Promise<void>

beforeEach(() => {
  requests = []
  historyStatus = 204
  historyAnswer = Promise.resolve()
  server.use(
    http.get("*/api/instances", () => HttpResponse.json([instance])),
    http.get("*/api/cross-seed/routes", () => HttpResponse.json([])),
    http.delete("*/api/instances/1", () => {
      requests.push("DELETE /api/instances/1")
      return HttpResponse.json({ message: "Instance deleted successfully" })
    }),
    http.delete("*/api/transfer-history", async ({ request }) => {
      requests.push(`DELETE /api/transfer-history${new URL(request.url).search}`)
      await historyAnswer
      return historyStatus === 204 ? new HttpResponse(null, { status: 204 }) : HttpResponse.json({ error: "Failed to delete transfer history" }, { status: historyStatus })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

async function removeInstance(tick: boolean) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <InstanceCard instance={instance} onEdit={() => {}} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const menu = document.querySelector("[aria-haspopup='menu']")
  if (!menu) throw new Error("no instance menu")
  fireEvent.pointerDown(menu, { button: 0, ctrlKey: false })
  fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }))

  const checkbox = await screen.findByRole("checkbox", { name: "Also delete its transfer history" })
  expect(checkbox.getAttribute("aria-checked")).toBe("false")
  if (tick) {
    fireEvent.click(checkbox)
  }
  const confirm = await screen.findByRole<HTMLButtonElement>("button", { name: "Delete" })
  await waitFor(() => expect(confirm.disabled).toBe(false))
  fireEvent.click(confirm)
}

describe("the instance removal dialog", () => {
  it("keeps the history without the tick", async () => {
    await removeInstance(false)

    await waitFor(() => expect(toast.success).toHaveBeenCalled())
    expect(requests).toEqual(["DELETE /api/instances/1"])
  })

  it("deletes the history of the instance with the tick", async () => {
    await removeInstance(true)

    await waitFor(() => expect(requests).toEqual(["DELETE /api/instances/1", "DELETE /api/transfer-history?instanceId=1"]))
    expect(toast.error).not.toHaveBeenCalled()
  })

  it("says that the history stays when its delete fails", async () => {
    historyStatus = 500
    await removeInstance(true)

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
      "The instance is deleted, but qui could not delete all of its history. Look in Settings > Statistics, and delete the rest there.",
      expect.anything()
    ))
    expect(toast.success).toHaveBeenCalled()
  })

  // regression: #4101
  it("warns about the history when the card is gone before the history delete fails", async () => {
    historyStatus = 500
    let answer = () => {}
    historyAnswer = new Promise(resolve => { answer = resolve })
    await removeInstance(true)
    await waitFor(() => expect(requests).toEqual(["DELETE /api/instances/1", "DELETE /api/transfer-history?instanceId=1"]))

    // A refetch of the instance list removes the card of the removed instance.
    cleanup()
    answer()

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
      "The instance is deleted, but qui could not delete all of its history. Look in Settings > Statistics, and delete the rest there.",
      expect.anything()
    ))
  })
})
