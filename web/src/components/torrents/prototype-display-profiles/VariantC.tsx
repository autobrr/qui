/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). Variant C: a chip in the bottom status bar, next to the
// Table/Dense view toggle (row density is part of a profile). It opens upward into
// a popover where each profile row says what it holds.

import { useNavigate } from "@tanstack/react-router"
import { ChevronUp, MonitorSmartphone, Plus, Settings2 } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { NewProfileForm } from "./NewProfileForm"
import { ProfileName } from "./ProfileName"
import { PrototypeNotice } from "./PrototypeNotice"
import { actions, summarize, useActiveProfile, usePrototype } from "./store"

export function VariantCStatusChip() {
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
      <PrototypeNotice enabled side="top">
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className={cn(
              "h-6 max-w-[10rem] gap-1 rounded-full border px-2 text-xs",
              open ? "bg-accent text-foreground" : "text-muted-foreground hover:text-accent-foreground"
            )}
            aria-label={`Display profile: ${active.name}`}
          >
            <MonitorSmartphone className="h-3 w-3" />
            <span className="truncate">{active.name}</span>
            <ChevronUp className="h-3 w-3 opacity-60" />
          </Button>
        </PopoverTrigger>
      </PrototypeNotice>
      <PopoverContent side="top" align="end" sideOffset={8} className="w-[min(23rem,calc(100vw-1rem))] p-0 [overflow-wrap:anywhere]" data-prototype-picker="">
        <div className="border-b px-3 py-2.5">
          <p className="text-sm font-semibold">Display profiles</p>
          <p className="text-xs text-muted-foreground">
            This browser uses <span className="font-medium text-foreground">{active.name}</span>. Each browser picks its own.
          </p>
        </div>

        <div role="radiogroup" aria-label="Display profile" className="flex max-h-72 flex-col gap-1 overflow-y-auto p-2">
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
                  "flex items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
                  isActive ? "border-primary/50 bg-primary/10" : "border-transparent hover:bg-accent"
                )}
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    "mt-1 flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full border",
                    isActive ? "border-primary" : "border-muted-foreground/50"
                  )}
                >
                  {isActive && <span className="h-1.5 w-1.5 rounded-full bg-primary" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <ProfileName profile={profile} className={cn("text-sm", isActive && "font-medium")} />
                    {isActive && (
                      <span className="shrink-0 rounded bg-primary/15 px-1 text-[10px] font-medium uppercase tracking-wide text-primary">
                        This browser
                      </span>
                    )}
                  </span>
                  <span className="block text-xs text-muted-foreground">{summarize(profile, { stretch: false })}</span>
                </span>
              </button>
            )
          })}
        </div>

        {!active.isDefault && (
          <div className="border-t px-3 py-2.5">
            <label className="flex cursor-pointer items-center gap-3">
              <span className="min-w-0 flex-1">
                <span className="block text-sm">Own sort and filters for {active.name}</span>
                <span className="block text-xs text-muted-foreground">
                  {active.ownSortAndFilters ? "Turning this off deletes them." : "Off: sort and filters follow every profile."}
                </span>
              </span>
              <Switch
                checked={!!active.ownSortAndFilters}
                onCheckedChange={(on) => actions.setOwnSortAndFilters(active.id, on)}
              />
            </label>
          </div>
        )}

        <div className="flex flex-col border-t p-1.5">
          {creating ? (
            <NewProfileForm className="p-1.5" onDone={() => setCreating(false)} />
          ) : (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-sm outline-none hover:bg-accent focus-visible:bg-accent"
            >
              <Plus className="h-4 w-4 shrink-0" />
              <span className="truncate">New profile from this setup…</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              actions.setPickerOpen(false)
              void navigate({ to: "/settings", search: { tab: "display-profiles" } })
            }}
            className="flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-sm text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
          >
            <Settings2 className="h-4 w-4 shrink-0" />
            <span className="truncate">Manage profiles…</span>
          </button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
