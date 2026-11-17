/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutationState, type MutationKey } from "@tanstack/react-query"

/**
 * The ids of the items that a mutation with this key still runs for, so that a list can show a spinner on each of them
 * and hold off a second press. The variables of the mutation are the id of the item, or an object with an `id`.
 */
export function usePendingIds(mutationKey: MutationKey): ReadonlySet<number> {
  const ids = useMutationState({
    filters: { mutationKey, status: "pending" },
    select: (mutation) => mutation.state.variables as number | { id: number },
  })
  return new Set(ids.map((variables) => typeof variables === "number" ? variables : variables.id))
}
