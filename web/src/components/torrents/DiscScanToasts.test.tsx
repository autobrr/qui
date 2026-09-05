/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ActivityEvent, DiscScanRun } from "@/types"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const { getDiscScan, toast, listeners, translation, syncStreamManager } = vi.hoisted(() => {
  const listeners = new Set<(event: ActivityEvent) => void>()
  return {
    getDiscScan: vi.fn(),
    toast: { success: vi.fn(), error: vi.fn() },
    listeners,
    translation: { t: (key: string, options?: { error?: string }) => options?.error ? `${key}:${options.error}` : key },
    syncStreamManager: {
      subscribeActivity: (listener: (event: ActivityEvent) => void) => {
        listeners.add(listener)
        return () => listeners.delete(listener)
      },
    },
  }
})
vi.mock("@/lib/api", () => ({ api: { getDiscScan } }))
vi.mock("sonner", () => ({ toast }))
vi.mock("react-i18next", () => ({ useTranslation: () => translation }))
vi.mock("@/contexts/SyncStreamContext", () => ({ useSyncStreamManager: () => syncStreamManager }))

import { DiscScanToasts } from "./DiscScanToasts"

const run = (status: DiscScanRun["status"], errorMessage?: string): DiscScanRun => ({
  id: 7, instanceId: 1, torrentHash: "abc", discPath: "Movie", resolvedPath: "/data/Movie", status, errorMessage,
  processedBytes: 0, totalBytes: 0, createdAt: "2026-01-01T00:00:00Z",
})
const emit = (event: Partial<ActivityEvent>) => act(() => {
  for (const listener of listeners) listener({ kind: "discscan.run", instanceId: 1, resourceId: "7", timestamp: "now", ...event })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  listeners.clear()
})

describe("DiscScanToasts", () => {
  it("toasts a completed run and the error of a failed run", async () => {
    render(<DiscScanToasts />)
    getDiscScan.mockResolvedValueOnce(run("completed"))
    await emit({})
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("discScan.toast.completed"))
    expect(getDiscScan).toHaveBeenCalledWith(1, 7)

    getDiscScan.mockResolvedValueOnce({ ...run("failed", "interrupted by qui restart"), id: 8 })
    await emit({ resourceId: "8" })
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("discScan.toast.failed:interrupted by qui restart"))
  })

  it("toasts once when a progress read lands after completion and the completion event follows", async () => {
    render(<DiscScanToasts />)
    getDiscScan.mockResolvedValue(run("completed"))
    await emit({})
    await emit({})
    await waitFor(() => expect(getDiscScan).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
  })

  it("stays silent for progress, cancel, and other event kinds", async () => {
    render(<DiscScanToasts />)
    getDiscScan.mockResolvedValueOnce(run("scanning")).mockResolvedValueOnce(run("canceled"))
    await emit({})
    await emit({})
    await emit({ kind: "backup.run" })
    await waitFor(() => expect(getDiscScan).toHaveBeenCalledTimes(2))
    expect(toast.success).not.toHaveBeenCalled()
    expect(toast.error).not.toHaveBeenCalled()
  })
})
