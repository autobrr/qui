/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutationState, type MutationKey } from "@tanstack/react-query"

export function usePendingIds(mutationKey: MutationKey): ReadonlySet<number> {
  const ids = useMutationState({
    filters: { mutationKey, status: "pending" },
    select: (mutation) => mutation.state.variables as number | { id: number },
  })
  return new Set(ids.map((variables) => typeof variables === "number" ? variables : variables.id))
}
