/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the top of the Toggle columns menu in every variant. The
// heading names what a change touches; the switch picks one Column setup for
// every instance of the profile, or one for each instance.

import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { actions, instanceLabel, sameColumns, useActiveProfile } from "./store"

export function ColumnsMenuHeader({ instanceId }: { instanceId: number }) {
  const active = useActiveProfile()
  const same = sameColumns(active)
  const here = instanceLabel(instanceId)

  return (
    <>
      <div className="px-2 pb-1 pt-1.5 text-xs font-medium text-muted-foreground" data-prototype-columns-heading="">
        Columns in <span className="text-foreground">{active.name}</span>
        {" · "}
        <span className="text-foreground">{same ? "every instance" : `${here} only`}</span>
      </div>
      <DropdownMenuItem
        onSelect={(e) => {
          e.preventDefault()
          actions.setSameColumns(active.id, !same, instanceId)
        }}
        className="items-start gap-3"
      >
        <span className="flex-1">
          <span className="block">Same columns on every instance</span>
          <span className="block text-xs text-muted-foreground">
            {same
              ? "Sort and column filters stay per instance."
              : `Turning this on gives every instance the ${here} columns.`}
          </span>
        </span>
        <Switch checked={same} tabIndex={-1} aria-hidden="true" className="pointer-events-none mt-0.5" />
      </DropdownMenuItem>
    </>
  )
}
