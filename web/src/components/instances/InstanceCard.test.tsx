/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { InstanceResponse } from "@/types"

import { InstanceCard } from "./InstanceCard"

const instances = vi.hoisted(() => ({
  instances: [
    { id: 1, name: "seedbox", hardlinkBaseDir: "" },
    { id: 2, name: "library", hardlinkBaseDir: "/data/links" },
  ],
  deleteInstance: () => {},
  testConnection: () => {},
  setInstanceStatus: () => {},
  isDeleting: false,
  isTesting: false,
  isUpdatingStatus: false,
  updatingStatusId: undefined,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))

afterEach(cleanup)

describe("InstanceCard", () => {
  it("names the Source instances that route to the instance in the delete dialog", async () => {
    server.use(
      http.get("*/api/instances", () => HttpResponse.json(instances.instances)),
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 2, crossSeedInstanceId: 1, rows: [] }])),
      http.get("*/api/cross-seed/routes/2", () => HttpResponse.json({
        sourceInstanceId: 2, crossSeedInstanceId: 1, rows: [{ baseDir: "/data/links", mappedPath: "", routedTorrents: null }],
      }))
    )
    const instance = { id: 1, name: "seedbox", host: "http://127.0.0.1:8080", isActive: true, connected: true } as InstanceResponse
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
    const deleteButton = await screen.findByRole<HTMLButtonElement>("button", { name: "Delete" })
    expect(deleteButton.disabled).toBe(true)

    expect(await screen.findByText("library adds cross-seeds to itself again after this delete.")).toBeTruthy()
    expect(await screen.findByText("seedbox is unavailable, so the number of routed torrents in the affected base folders of library is unknown.")).toBeTruthy()
    expect(deleteButton.disabled).toBe(false)
  })
})
