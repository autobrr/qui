/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api, APIError } from "@/lib/api"
import type { SSHCredentials, SSHTestResponse } from "@/types"
import { useCallback, useReducer } from "react"

// The SSH setup flow of one instance: save or clear credentials, test the
// connection, and pin or replace the host key the test presented. One state at
// a time: a request in flight has no error beside it. A result survives a
// failed pin, save or clear so the user still sees what the host presented; a
// failed test drops it, since the error now answers for the connection.
export type SSHSetupState =
  | { kind: "idle"; result: SSHTestResponse | null; error: string | null }
  | { kind: "saving"; result: SSHTestResponse | null }
  | { kind: "testing"; result: SSHTestResponse | null }
  | { kind: "pinning"; result: SSHTestResponse }

export type SSHSetupAction =
  | { type: "save" }
  | { type: "saved" }
  | { type: "test" }
  | { type: "tested"; result: SSHTestResponse }
  | { type: "pin" }
  | { type: "testFailed"; error: string }
  | { type: "failed"; error: string }

export const initialSSHSetupState: SSHSetupState = { kind: "idle", result: null, error: null }

export function sshSetupReducer(state: SSHSetupState, action: SSHSetupAction): SSHSetupState {
  switch (action.type) {
    case "save":
      return { kind: "saving", result: state.result }
    case "saved":
      // The credentials changed, so the last test answers for another setup.
      return { kind: "idle", result: null, error: null }
    case "test":
      return { kind: "testing", result: state.result }
    case "tested":
      return { kind: "idle", result: action.result, error: null }
    case "pin":
      return state.result ? { kind: "pinning", result: state.result } : state
    case "testFailed":
      return { kind: "idle", result: null, error: action.error }
    case "failed":
      return { kind: "idle", result: state.result, error: action.error }
  }
}

// The pin routes' 409 texts for a key or endpoint that moved under the request.
function isHostKeyConflict(message: string): boolean {
  return /host key|endpoint changed|different key/i.test(message)
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

interface UseSSHSetupOptions {
  instanceId: number
  // Called after every write, so the caller refreshes the instance row.
  onChanged: () => Promise<unknown> | void
}

export function useSSHSetup({ instanceId, onChanged }: UseSSHSetupOptions) {
  const [state, dispatch] = useReducer(sshSetupReducer, initialSSHSetupState)

  // The test route writes the tier it probed, so the row is refreshed before
  // the result shows: the probe table reads the tier from the instance.
  const test = useCallback(async () => {
    dispatch({ type: "test" })
    try {
      const result = await api.testSSH(instanceId)
      await onChanged()
      dispatch({ type: "tested", result })
    } catch (error) {
      dispatch({ type: "testFailed", error: errorText(error) })
    }
  }, [instanceId, onChanged])

  const save = useCallback(async (credentials: SSHCredentials) => {
    dispatch({ type: "save" })
    try {
      await api.setSSHCredentials(instanceId, credentials)
      await onChanged()
      dispatch({ type: "saved" })
      return true
    } catch (error) {
      dispatch({ type: "failed", error: errorText(error) })
      return false
    }
  }, [instanceId, onChanged])

  const clear = useCallback(async () => {
    dispatch({ type: "save" })
    try {
      await api.clearSSHCredentials(instanceId)
      await onChanged()
      dispatch({ type: "saved" })
      return true
    } catch (error) {
      dispatch({ type: "failed", error: errorText(error) })
      return false
    }
  }, [instanceId, onChanged])

  // Confirm pins the key the test presented; replace swaps a pin for it. The
  // server re-dials and compares, so a 409 about the key or the endpoint
  // (already pinned, not pinned, endpoint changed, or the host presented yet
  // another key) is answered by testing again: the next result shows the
  // state the row is in now. Any other refusal, such as a running orphan-scan
  // delete, is shown as it is.
  const pin = useCallback(async (replace: boolean) => {
    const hostKey = state.result?.hostKey
    if (!hostKey) {
      return
    }
    dispatch({ type: "pin" })
    try {
      if (replace) {
        await api.replaceSSHHostKey(instanceId, hostKey)
      } else {
        await api.confirmSSHHostKey(instanceId, hostKey)
      }
    } catch (error) {
      if (!(error instanceof APIError && error.status === 409 && isHostKeyConflict(error.message))) {
        dispatch({ type: "failed", error: errorText(error) })
        return
      }
    }
    await onChanged()
    await test()
  }, [instanceId, onChanged, state.result?.hostKey, test])

  return { state, test, save, clear, pin }
}
