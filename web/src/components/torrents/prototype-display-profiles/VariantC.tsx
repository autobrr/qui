/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). Variant C: a chip in the bottom status bar, next to the
// Table/Dense view toggle (row density is part of a profile). It opens upward
// into a menu in the style of the quick theme menu.

import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { Link } from "@tanstack/react-router"
import { Check, ChevronUp, Plus, Settings2 } from "lucide-react"
import { useEffect, useRef } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { ProfileGlyph, ProfileTile } from "./look"
import { openNewProfileDialog } from "./NewProfileDialog"
import { ProfileName } from "./ProfileName"
import { PrototypeNotice } from "./PrototypeNotice"
import { actions, useActiveProfile, usePrototype } from "./store"

export function VariantCStatusChip() {
  const open = usePrototype((s) => s.pickerOpen)
  const profiles = usePrototype((s) => s.profiles)
  const active = useActiveProfile()
  const wide = useMediaQuery("(min-width: 1024px)")
  const chipRef = useRef<HTMLButtonElement>(null)
  const checkedRef = useRef<HTMLDivElement>(null)
  const newRequested = useRef(false)

  // Radix focuses the menu, or its first item; start on this browser's profile instead.
  useEffect(() => {
    if (!open) return
    const frame = requestAnimationFrame(() => checkedRef.current?.focus())
    return () => cancelAnimationFrame(frame)
  }, [open])

  return (
    <DropdownMenu open={open} onOpenChange={actions.setPickerOpen}>
      <Tooltip disableHoverableContent>
        <PrototypeNotice enabled side="top">
          <TooltipTrigger asChild onFocus={(e) => e.preventDefault()}>
            <DropdownMenuTrigger asChild>
              <Button
                ref={chipRef}
                variant="ghost"
                size="sm"
                className={cn(
                  "h-6 gap-1 rounded-full border px-1.5 text-xs lg:px-2",
                  open ? "bg-accent text-foreground" : "text-muted-foreground hover:text-accent-foreground"
                )}
                aria-label={`Display profile: ${active.name}`}
              >
                <ProfileGlyph profile={active} />
                <span className="hidden max-w-[8rem] truncate lg:inline">{active.name}</span>
                <ChevronUp className="hidden h-3 w-3 opacity-60 lg:block" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
        </PrototypeNotice>
        {!wide && !open && <TooltipContent>{active.name}</TooltipContent>}
      </Tooltip>

      <DropdownMenuContent
        side="top"
        align="end"
        sideOffset={8}
        className="w-64 max-w-[calc(100vw-1rem)]"
        data-prototype-picker=""
        onCloseAutoFocus={(e) => {
          if (!newRequested.current) return
          newRequested.current = false
          e.preventDefault()
          openNewProfileDialog(chipRef.current)
        }}
      >
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">Display profile</DropdownMenuLabel>
        <DropdownMenuPrimitive.RadioGroup value={active.id} onValueChange={actions.pick} className="max-h-[min(18rem,calc(var(--radix-dropdown-menu-content-available-height)-6rem))] overflow-y-auto">
          {profiles.map((profile) => (
            <DropdownMenuPrimitive.RadioItem
              key={profile.id}
              ref={profile.id === active.id ? checkedRef : undefined}
              value={profile.id}
              textValue={profile.name}
              title={profile.name}
              className="flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[state=checked]:font-medium"
            >
              <ProfileTile profile={profile} size="sm" />
              <ProfileName profile={profile} className="flex-1" />
              <span className="flex size-4 shrink-0 items-center justify-center">
                <DropdownMenuPrimitive.ItemIndicator>
                  <Check aria-hidden="true" className="size-4" />
                </DropdownMenuPrimitive.ItemIndicator>
              </span>
            </DropdownMenuPrimitive.RadioItem>
          ))}
        </DropdownMenuPrimitive.RadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => (newRequested.current = true)}>
          <Plus />
          New profile from this setup…
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/settings" search={{ tab: "display-profiles" }}>
            <Settings2 />
            Manage profiles…
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
