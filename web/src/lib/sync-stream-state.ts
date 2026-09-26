import type { StreamState } from "@/contexts/SyncStreamContext"

export function isStreamUsable(state: StreamState): boolean {
  return state.connected && state.initialized && !state.dataStalled && !state.error
}
