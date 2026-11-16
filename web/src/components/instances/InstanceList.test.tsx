/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { InstanceResponse } from "@/types"

import { InstanceList } from "./InstanceList"

const instances = vi.hoisted(() => ({
  instances: [
    { id: 1, name: "seedbox", hardlinkBaseDir: "" },
    { id: 2, name: "library", hardlinkBaseDir: "/data/links" },
  ],
  deleteInstance: () => {},
  testConnection: () => {},
  setInstanceStatus: () => {},
  reorderInstances: () => {},
  isDeleting: false,
  isReordering: false,
  updatingStatusId: null,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))


// The switch of a row measures itself, and jsdom has no ResizeObserver.
beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderList(instance: InstanceResponse) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <InstanceList instances={[instance]} onEdit={() => {}} onAdd={() => {}} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe("InstanceList", () => {
  it.each([
    ["local", "Local files"],
    ["remote", "Files over SSH"],
    ["none", "No file access"],
  ] as const)("the row of an instance names the %s file access", (filesystemMode, text) => {
    renderList({ id: 1, name: "seedbox", host: "http://127.0.0.1:8080", isActive: true, connected: true, filesystemMode, sshTier: "sftp" } as InstanceResponse)
    expect(screen.getByRole("listitem").textContent).toContain(text)
  })

  it("names the Source instances that route to the instance in the delete dialog", async () => {
    server.use(
      http.get("*/api/instances", () => HttpResponse.json(instances.instances)),
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 2, crossSeedInstanceId: 1, rows: [] }])),
      http.get("*/api/cross-seed/routes/2", () => HttpResponse.json({
        sourceInstanceId: 2, crossSeedInstanceId: 1, rows: [{ baseDir: "/data/links", mappedPath: "", routedTorrents: null }],
      }))
    )
    renderList({ id: 1, name: "seedbox", host: "http://127.0.0.1:8080", isActive: true, connected: true } as InstanceResponse)

    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    const deleteButton = await screen.findByRole<HTMLButtonElement>("button", { name: "Delete" })
    expect(deleteButton.disabled).toBe(true)

    expect(await screen.findByText("library adds cross-seeds to itself again after this delete.")).toBeTruthy()
    expect(await screen.findByText("seedbox is unavailable, so the number of routed torrents in the affected base folders of library is unknown.")).toBeTruthy()
    expect(deleteButton.disabled).toBe(false)
  })
})
