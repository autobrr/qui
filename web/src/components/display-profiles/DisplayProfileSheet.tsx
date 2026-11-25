/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { Link } from "@tanstack/react-router"
import { Plus, Settings2 } from "lucide-react"
import { useState, type RefObject } from "react"
import { useTranslation } from "react-i18next"

import { useReturnFocus } from "@/components/kit/use-return-focus"
import { DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"

import { useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileList } from "./DisplayProfileList"
import { DisplayProfileTile } from "./DisplayProfileTile"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

export function DisplayProfileMenuItem({ onSelect }: { onSelect: () => void }) {
  const { t } = useTranslation("torrents")
  const { active } = useNamedDisplayProfiles()

  return (
    <>
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
  returnFocusRef: RefObject<HTMLElement | null>
}

export function DisplayProfileSheet({ open, onOpenChange, returnFocusRef }: DisplayProfileSheetProps) {
  const { t } = useTranslation("torrents")
  const { active, settled, pick } = useNamedDisplayProfiles()
  const [newProfileOpen, setNewProfileOpen] = useState(false)
  const returnFocus = useReturnFocus(returnFocusRef)

  const openNewProfile = () => {
    onOpenChange(false)
    setNewProfileOpen(true)
  }

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent
          side="bottom"
          className="max-h-[85dvh] gap-0 pb-[calc(0.75rem+env(safe-area-inset-bottom))]"
          onOpenAutoFocus={returnFocus.remember}
          onCloseAutoFocus={(event) => (newProfileOpen ? event.preventDefault() : returnFocus.restore(event))}
        >
          <SheetHeader className="pb-2">
            <SheetTitle>{t("displayProfiles.menuLabel")}</SheetTitle>
            <SheetDescription>{t("displayProfiles.sheetDescription")}</SheetDescription>
          </SheetHeader>
          <div className="min-h-0 overflow-y-auto px-4">
            <DisplayProfileList
              selectedId={active.id}
              rowsDisabled={!settled}
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
              disabled={!settled}
              onClick={openNewProfile}
              className="flex h-11 items-center gap-2 rounded-md px-2 text-sm outline-none hover:bg-accent focus-visible:bg-accent disabled:pointer-events-none disabled:opacity-50"
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
      <NewDisplayProfileDialog open={newProfileOpen} onOpenChange={setNewProfileOpen} returnFocusRef={returnFocusRef} />
    </>
  )
}
