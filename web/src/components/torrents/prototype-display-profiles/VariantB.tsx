/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). Variant B: no new button. The top of the existing
// Toggle columns menu becomes a row of profile chips; the column list below
// belongs to the active profile.

import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { Plus, Settings2 } from "lucide-react"
import { useState } from "react"

import { DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { NewProfileForm } from "./NewProfileForm"
import { actions, useActiveProfile, usePrototype } from "./store"

/** Replaces the "Toggle columns" label: the list below belongs to the active profile. */
export function VariantBColumnsLabel() {
  const active = useActiveProfile()
  return (
    <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
      Columns in <span className="text-foreground">{active.name}</span>
    </div>
  )
}

/** Rendered as the first children of the Toggle columns DropdownMenuContent. */
export function VariantBMenuSection() {
  const profiles = usePrototype((s) => s.profiles)
  const active = useActiveProfile()
  const [creating, setCreating] = useState(false)

  return (
    <>
      <div className="flex items-baseline justify-between px-2 pb-1 pt-1.5">
        <span className="text-xs font-medium text-muted-foreground">Display profile</span>
        <span className="text-[11px] text-muted-foreground/80">this browser</span>
      </div>
      <DropdownMenuPrimitive.RadioGroup
        value={active.id}
        onValueChange={actions.pick}
        className="flex flex-wrap gap-1 px-2 pb-2"
      >
        {profiles.map((profile) => (
          <DropdownMenuPrimitive.RadioItem
            key={profile.id}
            value={profile.id}
            // Keep the menu open: the column list below swaps to the picked profile.
            onSelect={(e) => e.preventDefault()}
            className={cn(
              "inline-flex h-7 max-w-[8.5rem] cursor-default items-center rounded-md border px-2.5 text-xs outline-none select-none",
              "focus:ring-2 focus:ring-ring/50",
              "data-[state=checked]:border-primary/60 data-[state=checked]:bg-primary/15 data-[state=checked]:font-medium data-[state=checked]:text-primary",
              "data-[state=unchecked]:border-foreground/15 data-[state=unchecked]:hover:bg-accent"
            )}
          >
            <span className="truncate">{profile.name}</span>
          </DropdownMenuPrimitive.RadioItem>
        ))}
        {!creating && (
          <DropdownMenuPrimitive.Item
            onSelect={(e) => {
              e.preventDefault()
              setCreating(true)
            }}
            aria-label="New profile from this setup…"
            className="inline-flex h-7 cursor-default items-center gap-1 rounded-md border border-dashed px-2 text-xs text-muted-foreground outline-none select-none hover:bg-accent focus:ring-2 focus:ring-ring/50"
          >
            <Plus className="h-3.5 w-3.5" />
            New
          </DropdownMenuPrimitive.Item>
        )}
      </DropdownMenuPrimitive.RadioGroup>

      {creating && (
        <div className="px-2 pb-2">
          <p className="pb-1 text-xs font-medium">New profile from this setup</p>
          <NewProfileForm onDone={() => setCreating(false)} />
        </div>
      )}

      {!active.isDefault && (
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault()
            actions.setOwnSortAndFilters(active.id, !active.ownSortAndFilters)
          }}
          className="items-start gap-3"
        >
          <span className="flex-1">
            <span className="block">Own sort and filters</span>
            <span className="block text-xs text-muted-foreground">
              {active.ownSortAndFilters ? "Only for this profile" : "Shared with every profile"}
            </span>
          </span>
          <Switch checked={active.ownSortAndFilters} tabIndex={-1} className="pointer-events-none mt-0.5" />
        </DropdownMenuItem>
      )}
      <DropdownMenuItem onSelect={() => actions.setManageOpen(true)} className="text-muted-foreground">
        <Settings2 />
        Manage profiles…
      </DropdownMenuItem>
      <DropdownMenuSeparator />
    </>
  )
}
