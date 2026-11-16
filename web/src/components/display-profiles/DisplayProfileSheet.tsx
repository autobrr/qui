/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { Link } from "@tanstack/react-router"
import { Plus, Settings2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useDisplayProfiles } from "@/lib/client-settings"

import { DisplayProfileList } from "./DisplayProfileList"
import { DisplayProfileTile } from "./DisplayProfileTile"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

/** "Display profile" with the profile of this browser, and a separator under it, for the menu of the mobile torrent list. */
export function DisplayProfileMenuItem({ onSelect }: { onSelect: () => void }) {
  const { t } = useTranslation("torrents")
  const { active } = useDisplayProfiles()

  return (
    <>
      {/* Not the ui item: it mutes each icon without a text color, and the tile icon has the profile color. */}
      <DropdownMenuPrimitive.Item
        onSelect={onSelect}
        className="relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground"
      >
        <DisplayProfileTile profile={active} size="sm" />
        <span className="shrink-0">{t("displayProfiles.menuLabel")}</span>
        <span className="ml-auto min-w-0 truncate text-muted-foreground">{active.name}</span>
      </DropdownMenuPrimitive.Item>
      <DropdownMenuSeparator />
    </>
  )
}

interface DisplayProfileSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** The profile list in a bottom sheet for a phone. A tap on a row picks that profile for this browser and closes the sheet. */
export function DisplayProfileSheet({ open, onOpenChange }: DisplayProfileSheetProps) {
  const { t } = useTranslation("torrents")
  const { active, pick } = useDisplayProfiles()
  const [newProfileOpen, setNewProfileOpen] = useState(false)

  const openNewProfile = () => {
    onOpenChange(false)
    setNewProfileOpen(true)
  }

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="bottom" className="max-h-[85dvh] gap-0 pb-[calc(0.75rem+env(safe-area-inset-bottom))]">
          <SheetHeader className="pb-2">
            <SheetTitle>{t("displayProfiles.menuLabel")}</SheetTitle>
            <SheetDescription>{t("displayProfiles.sheetDescription")}</SheetDescription>
          </SheetHeader>
          <div className="min-h-0 overflow-y-auto px-4">
            <DisplayProfileList
              selectedId={active.id}
              onSelect={(profile) => {
                pick(profile.id)
                onOpenChange(false)
              }}
              onNewProfile={openNewProfile}
            />
          </div>
          <div className="mt-2 flex flex-col border-t px-2 pt-2">
            <button
              type="button"
              onClick={openNewProfile}
              className="flex h-11 items-center gap-2 rounded-md px-2 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
            >
              <Plus aria-hidden="true" className="size-4 text-muted-foreground" />
              {t("displayProfiles.newProfile")}
            </button>
            <Link
              to="/settings"
              search={{ tab: "display-profiles" }}
              onClick={() => onOpenChange(false)}
              className="flex h-11 items-center gap-2 rounded-md px-2 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
            >
              <Settings2 aria-hidden="true" className="size-4 text-muted-foreground" />
              {t("displayProfiles.manage")}
            </Link>
          </div>
        </SheetContent>
      </Sheet>
      <NewDisplayProfileDialog open={newProfileOpen} onOpenChange={setNewProfileOpen} />
    </>
  )
}
