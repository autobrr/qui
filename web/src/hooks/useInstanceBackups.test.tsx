/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    triggerBackup: vi.fn(),
    importBackupManifest: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useImportBackupManifest, useTriggerBackup } from "@/hooks/useInstanceBackups"

const mockedApi = vi.mocked(api, true)

const run = { id: 7, instanceId: 1, status: "pending" } as never
const existingRun = { id: 3, instanceId: 1, status: "success" }
const orphanRuns = [{ id: 9, status: "completed" }]

function makeClient() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(["instance-backups", 1, "runs", 25, 0], { runs: [existingRun], hasMore: false })
  queryClient.setQueryData(["orphan-scan", 1, "runs", null], orphanRuns)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper }
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

type RunMutation = { mutateAsync: (payload: never) => Promise<unknown> }

async function expectPrependsRun(hook: () => RunMutation, payload: unknown) {
  mockedApi.triggerBackup.mockResolvedValue(run)
  mockedApi.importBackupManifest.mockResolvedValue(run)
  const { queryClient, wrapper } = makeClient()
  const { result } = renderHook(hook, { wrapper })

  await expect(result.current.mutateAsync(payload as never)).resolves.toEqual(run)

  expect(queryClient.getQueryData(["instance-backups", 1, "runs", 25, 0])).toEqual({ runs: [run, existingRun], hasMore: false })
  expect(queryClient.getQueryData(["orphan-scan", 1, "runs", null])).toBe(orphanRuns)
}

describe("backup run mutations", () => {
  it("useTriggerBackup prepends the run without touching other features' runs queries", async () => {
    await expectPrependsRun(() => useTriggerBackup(1), {})
  })

  it("useImportBackupManifest prepends the run without touching other features' runs queries", async () => {
    await expectPrependsRun(() => useImportBackupManifest(1), new File([], "m.json"))
  })
})
