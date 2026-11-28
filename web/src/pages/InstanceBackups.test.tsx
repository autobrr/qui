/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { delay, HttpResponse, http } from "msw"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { server } from "@/test/msw/server"

const stubs = vi.hoisted(() => ({
  translation: { t: (key: string) => key, i18n: { language: "en", resolvedLanguage: "en" } },
  selectInstance: (() => {}) as (id: number) => void,
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => stubs.translation,
}))

vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/contexts/SyncStreamContext")>(),
  useActivityStream: () => {},
}))

vi.mock("@/hooks/usePersistedInstanceSelection", () => ({
  usePersistedInstanceSelection: () => {
    const [id, setId] = useState<number | undefined>(1)
    stubs.selectInstance = setId
    return [id, setId] as const
  },
}))

import { InstanceBackups } from "./InstanceBackups"

afterEach(cleanup)

const settings = (instanceId: number) => ({
  instanceId,
  enabled: true,
  hourlyEnabled: false,
  dailyEnabled: true,
  weeklyEnabled: false,
  monthlyEnabled: false,
  keepHourly: 0,
  keepDaily: 7,
  keepWeekly: 4,
  keepMonthly: 12,
  includeCategories: true,
  includeTags: true,
  includeSavePaths: true,
})

describe("InstanceBackups", () => {
  it("keeps Save disabled while the next instance's settings load", async () => {
    server.use(
      http.get("*/api/instances", () => HttpResponse.json([
        { id: 1, name: "A", host: "http://a.test", connected: true },
        { id: 2, name: "B", host: "http://b.test", connected: true },
      ])),
      http.get("*/api/instances/:id/capabilities", () => HttpResponse.json({ supportsTorrentExport: true })),
      http.get("*/api/instances/:id/backups/runs", () => HttpResponse.json({ runs: [], hasMore: false })),
      http.get("*/api/instances/:id/backups/settings", async ({ params }) => {
        if (params.id === "2") await delay("infinite")
        return HttpResponse.json(settings(Number(params.id)))
      })
    )
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
    render(
      <RouterContextProvider router={router}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <InstanceBackups />
          </TooltipProvider>
        </QueryClientProvider>
      </RouterContextProvider>
    )
    const save = () => screen.getByRole("button", { name: "backups.settings.actions.saveChanges" })
    await waitFor(() => expect(save().hasAttribute("disabled")).toBe(false))

    act(() => stubs.selectInstance(2))

    expect(save().hasAttribute("disabled")).toBe(true)
    expect(screen.getByRole("button", { name: "backups.settings.actions.saveAll" }).hasAttribute("disabled")).toBe(true)
  })

  it("shows planned torrent counts in dry-run summary and keeps excluded rows visible", async () => {
    const allAddTorrents = [
      { manifest: { hash: "aaa111", name: "First Torrent", sizeBytes: 1024 } },
      { manifest: { hash: "bbb222", name: "Second Torrent", sizeBytes: 2048 } },
      { manifest: { hash: "ccc333", name: "Third Torrent", sizeBytes: 4096 } },
    ]
    const planFor = (excludeHashes: string[] = []) => ({
      mode: "incremental" as const,
      runId: 10,
      instanceId: 1,
      categories: { create: [{ name: "movies", savePath: "/movies" }] },
      tags: { create: [{ name: "hd" }] },
      torrents: {
        add: allAddTorrents.filter(item => !excludeHashes.includes(item.manifest.hash)),
      },
    })

    let previewCount = 0
    let lastPreviewBody: { excludeHashes?: string[] } | undefined

    server.use(
      http.get("*/api/instances", () => HttpResponse.json([
        { id: 1, name: "A", host: "http://a.test", connected: true },
      ])),
      http.get("*/api/instances/:id/capabilities", () => HttpResponse.json({ supportsTorrentExport: true })),
      http.get("*/api/instances/:id/backups/settings", () => HttpResponse.json(settings(1))),
      http.get("*/api/instances/:id/backups/runs", () => HttpResponse.json({
        runs: [
          {
            id: 10,
            instanceId: 1,
            kind: "manual",
            status: "success",
            requestedBy: "ui",
            requestedAt: "2026-11-27T00:00:00Z",
            completedAt: "2026-11-27T00:01:00Z",
            totalBytes: 3072,
            torrentCount: 2,
          },
        ],
        hasMore: false,
      })),
      http.post("*/api/instances/:id/backups/runs/:runId/restore/preview", async ({ request }) => {
        const body = await request.json() as { excludeHashes?: string[] }
        previewCount += 1
        lastPreviewBody = body
        return HttpResponse.json(planFor(body.excludeHashes))
      }),
      http.post("*/api/instances/:id/backups/runs/:runId/restore", async ({ request }) => {
        const body = await request.json() as { dryRun?: boolean; excludeHashes?: string[] }
        return HttpResponse.json({
          mode: "incremental",
          runId: 10,
          instanceId: 1,
          dryRun: Boolean(body.dryRun),
          plan: planFor(body.excludeHashes),
          applied: { categories: {}, tags: {}, torrents: {} },
        })
      })
    )

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
    render(
      <RouterContextProvider router={router}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <InstanceBackups />
          </TooltipProvider>
        </QueryClientProvider>
      </RouterContextProvider>
    )

    const restoreBtn = await screen.findByRole("button", { name: "backups.history.restoreFromLatest" })
    await waitFor(() => expect(restoreBtn.hasAttribute("disabled")).toBe(false))
    fireEvent.click(restoreBtn)

    await screen.findByText("First Torrent")
    await screen.findByText("Second Torrent")

    const excludeButtons = screen.getAllByRole("button", { name: "backups.restore.excludeAria" })
    fireEvent.click(excludeButtons[0])

    const runDryRunBtn = screen.getByRole("button", { name: "backups.restore.runDryRun" })
    fireEvent.click(runDryRunBtn)

    await screen.findByText("backups.restore.lastExecution")
    const summaryText = screen.getByText("backups.restore.lastExecution").closest("div")?.parentElement?.textContent ?? ""
    expect(summaryText).toContain("+1 / Δ0 / −0")
    expect(summaryText).toContain("+1 / −0")
    expect(summaryText).toContain("+2 / Δ0 / −0")
    expect(screen.queryByText("First Torrent")).not.toBeNull()
    expect(screen.queryByRole("button", { name: "backups.restore.includeAria" })).not.toBeNull()

    const refreshBtn = screen.getByRole("button", { name: "backups.restore.refreshPlan" })
    fireEvent.click(refreshBtn)
    await waitFor(() => {
      expect(previewCount).toBe(2)
      expect(refreshBtn.hasAttribute("disabled")).toBe(false)
    })
    expect(lastPreviewBody?.excludeHashes).toBeUndefined()
    expect(screen.queryByText("First Torrent")).not.toBeNull()
    expect(screen.queryByRole("button", { name: "backups.restore.includeAria" })).not.toBeNull()
  })

  it("discards stale preview responses after closing and opening another backup run", async () => {
    let resolveRun10Preview: ((value: Response) => void) | undefined

    server.use(
      http.get("*/api/instances", () => HttpResponse.json([
        { id: 1, name: "A", host: "http://a.test", connected: true },
      ])),
      http.get("*/api/instances/:id/capabilities", () => HttpResponse.json({ supportsTorrentExport: true })),
      http.get("*/api/instances/:id/backups/settings", () => HttpResponse.json(settings(1))),
      http.get("*/api/instances/:id/backups/runs", () => HttpResponse.json({
        runs: [
          {
            id: 10,
            instanceId: 1,
            kind: "manual",
            status: "success",
            requestedBy: "ui",
            requestedAt: "2026-11-27T01:00:00Z",
            completedAt: "2026-11-27T01:01:00Z",
            totalBytes: 1024,
            torrentCount: 1,
          },
          {
            id: 11,
            instanceId: 1,
            kind: "daily",
            status: "success",
            requestedBy: "ui",
            requestedAt: "2026-11-26T01:00:00Z",
            completedAt: "2026-11-26T01:01:00Z",
            totalBytes: 2048,
            torrentCount: 1,
          },
        ],
        hasMore: false,
      })),
      http.post("*/api/instances/:id/backups/runs/:runId/restore/preview", ({ params }) => {
        if (params.runId === "10") {
          return new Promise<Response>((resolve) => {
            resolveRun10Preview = resolve
          })
        }
        return HttpResponse.json({
          mode: "incremental",
          runId: 11,
          instanceId: 1,
          categories: {},
          tags: {},
          torrents: {
            add: [{ manifest: { hash: "run11hash", name: "Run 11 Torrent", sizeBytes: 2048 } }],
          },
        })
      })
    )

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
    render(
      <RouterContextProvider router={router}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <InstanceBackups />
          </TooltipProvider>
        </QueryClientProvider>
      </RouterContextProvider>
    )

    const restoreRowBtns = await screen.findAllByRole("button", { name: "backups.history.actions.restoreFromBackup" })
    fireEvent.click(restoreRowBtns[0])
    await waitFor(() => expect(resolveRun10Preview).toBeDefined())

    fireEvent.click(screen.getByRole("button", { name: "actions.close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    fireEvent.click(restoreRowBtns[1])
    await screen.findByText("Run 11 Torrent")

    fireEvent.click(screen.getByRole("button", { name: "backups.restore.excludeAria" }))
    expect(screen.queryByRole("button", { name: "backups.restore.includeAria" })).not.toBeNull()

    await act(async () => {
      resolveRun10Preview?.(HttpResponse.json({
        mode: "incremental",
        runId: 10,
        instanceId: 1,
        categories: {},
        tags: {},
        torrents: {
          add: [{ manifest: { hash: "run10hash", name: "Run 10 Torrent", sizeBytes: 1024 } }],
        },
      }))
    })

    expect(screen.queryByText("Run 11 Torrent")).not.toBeNull()
    expect(screen.queryByText("Run 10 Torrent")).toBeNull()
    expect(screen.queryByRole("button", { name: "backups.restore.includeAria" })).not.toBeNull()
  })
})
