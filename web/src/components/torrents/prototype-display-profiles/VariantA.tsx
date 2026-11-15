/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). Variant A: a toolbar button that names the active
// profile ("Laptop ▾"), left of Name stretch and Toggle columns, with a popover.

import { useNavigate } from "@tanstack/react-router"
import { Check, ChevronDown, MonitorSmartphone, Plus, Settings2 } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { NewProfileForm } from "./NewProfileForm"
import { PrototypeNotice } from "./PrototypeNotice"
import { actions, useActiveProfile, usePrototype } from "./store"

export function VariantAToolbarButton() {
  const open = usePrototype((s) => s.pickerOpen)
  const profiles = usePrototype((s) => s.profiles)
  const active = useActiveProfile()
  const [creating, setCreating] = useState(false)
  const navigate = useNavigate()

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        actions.setPickerOpen(next)
        if (!next) setCreating(false)
      }}
    >
      <PrototypeNotice enabled>
        <PopoverTrigger asChild>
          <Button variant="outline" className="h-9 max-w-[11rem] gap-1.5 px-2.5" aria-label={`Display profile: ${active.name}`}>
            <MonitorSmartphone className="h-4 w-4 shrink-0" />
            <span className="truncate text-sm">{active.name}</span>
            <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          </Button>
        </PopoverTrigger>
      </PrototypeNotice>
      <PopoverContent align="end" className="w-72 p-0" data-prototype-picker="">
        <div className="px-3 pb-1.5 pt-3">
          <p className="text-sm font-semibold">Display profile</p>
          <p className="text-xs text-muted-foreground">This browser only. Other browsers keep their own pick.</p>
        </div>
        <div role="radiogroup" aria-label="Display profile" className="flex flex-col px-1.5 pb-1.5">
          {profiles.map((profile) => {
            const isActive = profile.id === active.id
            return (
              <button
                key={profile.id}
                type="button"
                role="radio"
                aria-checked={isActive}
                onClick={() => actions.pick(profile.id)}
                className={cn(
                  "flex h-8 items-center gap-2 rounded-md px-2 text-left text-sm outline-none hover:bg-accent focus-visible:bg-accent",
                  isActive && "font-medium"
                )}
              >
                <Check className={cn("h-4 w-4 shrink-0", isActive ? "opacity-100" : "opacity-0")} />
                <span className="truncate">{profile.name}</span>
                {profile.ownSortAndFilters && (
                  <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">own sort</span>
                )}
              </button>
            )
          })}
        </div>

        <Separator />
        <div className="p-1.5">
          {creating ? (
            <NewProfileForm className="p-1.5" onDone={() => setCreating(false)} />
          ) : (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm outline-none hover:bg-accent focus-visible:bg-accent"
            >
              <Plus className="h-4 w-4" />
              New profile from this setup…
            </button>
          )}
        </div>

        {!active.isDefault && (
          <>
            <Separator />
            <div className="flex items-start gap-3 px-3 py-2.5">
              <div className="flex-1">
                <Label htmlFor="proto-a-own" className="text-sm">Own sort and filters</Label>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {active.ownSortAndFilters
                    ? `${active.name} keeps its own sort and filters.`
                    : `Off: ${active.name} shares sort and filters with every profile.`}
                </p>
              </div>
              <Switch
                id="proto-a-own"
                checked={!!active.ownSortAndFilters}
                onCheckedChange={(on) => actions.setOwnSortAndFilters(active.id, on)}
              />
            </div>
          </>
        )}

        <Separator />
        <div className="p-1.5">
          <button
            type="button"
            onClick={() => {
              actions.setPickerOpen(false)
              void navigate({ to: "/settings", search: { tab: "display-profiles" } })
            }}
            className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
          >
            <Settings2 className="h-4 w-4" />
            Manage profiles…
          </button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
