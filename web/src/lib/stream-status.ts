// Copyright (c) 2025, s0up and the autobrr contributors.

export type StreamFallbackVariant = "degraded" | "offline"

export interface StreamFallbackPlan {
  tone: "warning" | "error"
  variant: StreamFallbackVariant
  showStaleAge: boolean
}

export function resolveStreamFallbackStatus(args: {
  hasData: boolean
  hasLastSuccessfulSync: boolean
}): StreamFallbackPlan {
  if (!args.hasData) {
    return { tone: "error", variant: "offline", showStaleAge: false }
  }

  return { tone: "warning", variant: "degraded", showStaleAge: args.hasLastSuccessfulSync }
}
