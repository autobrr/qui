/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): "Display profile" in the menu of the mobile torrent
// list, and the sheet it opens. Menu item, then a row: a phone picks a profile
// in two taps once the menu is open. The sheet closes on a pick.

import { Link } from "@tanstack/react-router"
import { Plus, Settings2 } from "lucide-react"
import { useRef } from "react"

import { DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { ProfileTile } from "./look"
import { openNewProfileDialog } from "./NewProfileDialog"
import { ProfileList } from "./ProfileList"
import { actions, PROTOTYPE_ENABLED, useActiveProfile, usePrototype } from "./store"

export function MobileProfileMenuItem() {
  const active = useActiveProfile()
  if (!PROTOTYPE_ENABLED) return null
  return (
    <>
      <DropdownMenuItem
        data-prototype-mobile-item=""
        className="text-xs"
        onSelect={() => actions.setProfileSheetOpen(true)}
      >
        <ProfileTile profile={active} size="sm" />
        <span className="shrink-0">Display profile</span>
        <span className="ml-auto min-w-0 truncate text-muted-foreground">{active.name}</span>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
    </>
  )
}

export function MobileProfileSheet() {
  const open = usePrototype((s) => s.profileSheetOpen)
  const newRequested = useRef(false)

  return (
    <Sheet open={open} onOpenChange={actions.setProfileSheetOpen}>
      <SheetContent
        side="bottom"
        className="max-h-[85dvh] gap-0 pb-[calc(0.75rem+env(safe-area-inset-bottom))]"
        data-prototype-profile-sheet=""
        onCloseAutoFocus={(e) => {
          if (!newRequested.current) return
          newRequested.current = false
          e.preventDefault()
          openNewProfileDialog()
        }}
      >
        <SheetHeader className="pb-2">
          <SheetTitle>Display profile</SheetTitle>
          <SheetDescription>Each browser picks its own.</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 overflow-y-auto px-4">
          <ProfileList
            mode="pick"
            onActivate={(p) => {
              actions.pick(p.id)
              actions.setProfileSheetOpen(false)
            }}
          />
        </div>
        <div className="mt-2 flex flex-col border-t px-2 pt-2">
          <button
            type="button"
            onClick={() => {
              newRequested.current = true
              actions.setProfileSheetOpen(false)
            }}
            className="flex h-11 items-center gap-2 rounded-md px-2 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
          >
            <Plus className="size-4 text-muted-foreground" />
            New profile from this setup…
          </button>
          <Link
            to="/settings"
            search={{ tab: "display-profiles" }}
            onClick={() => actions.setProfileSheetOpen(false)}
            className="flex h-11 items-center gap-2 rounded-md px-2 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
          >
            <Settings2 className="size-4 text-muted-foreground" />
            Manage profiles…
          </Link>
        </div>
      </SheetContent>
    </Sheet>
  )
}
