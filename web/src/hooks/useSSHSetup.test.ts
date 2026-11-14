/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { SSHTestResponse } from "@/types"

const mocks = vi.hoisted(() => ({
  testSSH: vi.fn(),
  setSSHCredentials: vi.fn(),
  clearSSHCredentials: vi.fn(),
  confirmSSHHostKey: vi.fn(),
  replaceSSHHostKey: vi.fn(),
}))

vi.mock("@/lib/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/api")>(),
  api: mocks,
}))

import { APIError } from "@/lib/api"
import { initialSSHSetupState, sshSetupReducer, useSSHSetup, type SSHSetupAction, type SSHSetupState } from "./useSSHSetup"

afterEach(() => {
  vi.clearAllMocks()
})

const unpinned: SSHTestResponse = { status: "unpinned", hostKey: "AAAA", fingerprint: "SHA256:abc", keyType: "ssh-ed25519" }
const pinned: SSHTestResponse = { status: "pinned", hostKey: "AAAA", fingerprint: "SHA256:abc", keyType: "ssh-ed25519" }

describe("sshSetupReducer", () => {
  const withResult: SSHSetupState = { kind: "idle", result: unpinned, error: null }

  it.each<[string, SSHSetupState, SSHSetupAction, SSHSetupState]>([
    ["test keeps the last result while it runs", withResult, { type: "test" }, { kind: "testing", result: unpinned }],
    ["tested replaces the result and clears the error", { kind: "idle", result: null, error: "old" }, { type: "tested", result: pinned }, { kind: "idle", result: pinned, error: null }],
    ["a failed test drops the result, so the error shows alone", { kind: "testing", result: unpinned }, { type: "testFailed", error: "boom" }, { kind: "idle", result: null, error: "boom" }],
    ["a failed pin, save or clear keeps the result beside the error", { kind: "pinning", result: unpinned }, { type: "failed", error: "boom" }, { kind: "idle", result: unpinned, error: "boom" }],
    ["pin needs a result", initialSSHSetupState, { type: "pin" }, initialSSHSetupState],
    ["pin holds the result it pins", withResult, { type: "pin" }, { kind: "pinning", result: unpinned }],
    ["save keeps the result until it lands", withResult, { type: "save" }, { kind: "saving", result: unpinned }],
    ["saved drops the result, since it answered for other credentials", { kind: "saving", result: unpinned }, { type: "saved" }, initialSSHSetupState],
  ])("%s", (_name, state, action, want) => {
    expect(sshSetupReducer(state, action)).toEqual(want)
  })
})

describe("useSSHSetup", () => {
  const onChanged = vi.fn()

  it("test stores the response", async () => {
    mocks.testSSH.mockResolvedValue(unpinned)
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())

    expect(mocks.testSSH).toHaveBeenCalledWith(7)
    expect(onChanged).toHaveBeenCalledTimes(1)
    expect(result.current.state).toEqual({ kind: "idle", result: unpinned, error: null })
  })

  // regression: #3732
  it("a 409 from a running delete is shown, not retried", async () => {
    mocks.testSSH.mockResolvedValue({ status: "mismatch", hostKey: "BBBB", fingerprint: "SHA256:new", pinnedFingerprint: "SHA256:old" })
    mocks.replaceSSHHostKey.mockRejectedValue(new APIError("An orphan scan is deleting files on this instance. Wait for it to finish before changing its SSH settings.", 409))
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())
    await act(() => result.current.pin(true))

    expect(mocks.testSSH).toHaveBeenCalledTimes(1)
    expect(result.current.state.kind).toBe("idle")
    expect(result.current.state.kind === "idle" && result.current.state.error).toContain("orphan scan")
  })

  it("confirm posts the host key the test returned, refreshes, and tests again", async () => {
    mocks.testSSH.mockResolvedValueOnce(unpinned).mockResolvedValueOnce(pinned)
    mocks.confirmSSHHostKey.mockResolvedValue(undefined)
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())
    await act(() => result.current.pin(false))

    expect(mocks.confirmSSHHostKey).toHaveBeenCalledWith(7, "AAAA")
    expect(onChanged).toHaveBeenCalledTimes(3)
    expect(mocks.testSSH).toHaveBeenCalledTimes(2)
    expect(result.current.state.result).toEqual(pinned)
  })

  it("replace uses the replace route and ends on the new test's result", async () => {
    mocks.testSSH
      .mockResolvedValueOnce({ status: "mismatch", hostKey: "BBBB", fingerprint: "SHA256:new", pinnedFingerprint: "SHA256:old" })
      .mockResolvedValueOnce(pinned)
    mocks.replaceSSHHostKey.mockResolvedValue(undefined)
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())
    await act(() => result.current.pin(true))

    expect(mocks.replaceSSHHostKey).toHaveBeenCalledWith(7, "BBBB")
    expect(mocks.confirmSSHHostKey).not.toHaveBeenCalled()
    expect(result.current.state).toEqual({ kind: "idle", result: pinned, error: null })
  })

  it("a 409 on pin is answered by a new test, not an error", async () => {
    mocks.testSSH.mockResolvedValueOnce(unpinned).mockResolvedValueOnce(pinned)
    mocks.confirmSSHHostKey.mockRejectedValue(new APIError("ssh host key is already pinned", 409))
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())
    await act(() => result.current.pin(false))

    expect(mocks.testSSH).toHaveBeenCalledTimes(2)
    expect(result.current.state).toEqual({ kind: "idle", result: pinned, error: null })
  })

  it("another failure on pin keeps the result and shows the error", async () => {
    mocks.testSSH.mockResolvedValue(unpinned)
    mocks.confirmSSHHostKey.mockRejectedValue(new APIError("Could not connect to the SSH host", 502))
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    await act(() => result.current.test())
    await act(() => result.current.pin(false))

    expect(mocks.testSSH).toHaveBeenCalledTimes(1)
    expect(result.current.state).toEqual({ kind: "idle", result: unpinned, error: "Could not connect to the SSH host" })
  })

  it("save sends the credentials, refreshes, and forgets the last result", async () => {
    mocks.testSSH.mockResolvedValue(unpinned)
    mocks.setSSHCredentials.mockResolvedValue(undefined)
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))
    const credentials = { host: "box.example", port: 22, username: "qui", privateKey: "KEY" }

    await act(() => result.current.test())
    let ok = false
    await act(async () => { ok = await result.current.save(credentials) })

    expect(ok).toBe(true)
    expect(mocks.setSSHCredentials).toHaveBeenCalledWith(7, credentials)
    expect(onChanged).toHaveBeenCalledTimes(2)
    expect(result.current.state).toEqual(initialSSHSetupState)
  })

  it("a refused clear is an inline error", async () => {
    mocks.clearSSHCredentials.mockRejectedValue(new APIError("Disable hardlink or reflink mode on this instance before removing its SSH credentials", 400))
    const { result } = renderHook(() => useSSHSetup({ instanceId: 7, onChanged }))

    let ok = true
    await act(async () => { ok = await result.current.clear() })

    expect(ok).toBe(false)
    expect(onChanged).not.toHaveBeenCalled()
    expect(result.current.state).toEqual({ kind: "idle", result: null, error: "Disable hardlink or reflink mode on this instance before removing its SSH credentials" })
  })
})
