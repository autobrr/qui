/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway), v3: Settings › General › Display profiles. A list of
// profiles next to an editor. Below 768 px the list comes alone: a tap on a row
// picks the profile for this browser, and › opens the editor in a sheet.

import { Link } from "@tanstack/react-router"
import { Info, Palette, Plus } from "lucide-react"
import { Fragment, useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { openNewProfileDialog } from "./NewProfileDialog"
import { ProfileEditor } from "./ProfileEditor"
import { ProfileList } from "./ProfileList"
import { actions, usePrototype } from "./store"

const HOLDS = ["Columns", "Row density", "Sidebars", "Filter sections", "Category trees", "Empty filters"]

function HoldsInfo() {
  return (
    <Popover>
      <PopoverTrigger
        aria-label="More about Display profiles"
        className="inline-grid size-6 shrink-0 place-items-center rounded-full align-middle text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <Info className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent className="w-80 max-w-[calc(100vw-1rem)] space-y-2 text-sm">
        <p>
          A profile holds how the torrent list looks: which columns show, their order and widths, Name stretch, row
          density, open sidebars, open filter sections, collapsed category trees, and whether empty filters show.
        </p>
        <p className="text-muted-foreground">
          Each browser picks one profile, and only that browser remembers the pick. A browser that never picked one, or
          a private window, uses Default.
        </p>
        <p className="text-muted-foreground">
          Every profile shares the rest: language, units, date and time format, delete defaults, and the other settings
          on these pages.
        </p>
      </PopoverContent>
    </Popover>
  )
}

export function DisplayProfilesSettings() {
  const profiles = usePrototype((s) => s.profiles)
  const activeId = usePrototype((s) => s.activeProfileId)
  const wide = useMediaQuery("(min-width: 768px)")
  // The editor opens on this browser's profile; a deleted pick falls back to it.
  const [selectedId, setSelectedId] = useState(activeId)
  const lastCreatedId = usePrototype((s) => s.lastCreatedId)
  const createdBeforeMount = useRef(lastCreatedId)
  useEffect(() => {
    if (lastCreatedId && lastCreatedId !== createdBeforeMount.current) setSelectedId(lastCreatedId)
  }, [lastCreatedId])
  const shownId = profiles.some((p) => p.id === selectedId) ? selectedId : activeId
  const [sheetId, setSheetId] = useState<string | null>(null)
  const sheetProfile = profiles.find((p) => p.id === sheetId)

  return (
    <>
      <Card data-prototype-settings-section="">
        <CardHeader className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 px-4 sm:px-6">
          <div className="min-w-0 flex-[1_1_18rem] space-y-1.5">
            <CardTitle>Display profiles</CardTitle>
            <CardDescription>
              Holds:{" "}
              {HOLDS.map((item, i) => (
                <Fragment key={item}>
                  <span className="whitespace-nowrap">{i < HOLDS.length - 1 ? `${item}\u00a0·` : item}</span>{" "}
                </Fragment>
              ))}
              <HoldsInfo />
            </CardDescription>
          </div>
          <Button size="sm" variant="outline" className="shrink-0 max-sm:h-10 max-sm:w-full" onClick={(e) => openNewProfileDialog(e.currentTarget)}>
            <Plus />
            New profile from this setup
          </Button>
        </CardHeader>

        <CardContent className="px-4 sm:px-6">
          {wide ? (
            <div className="grid grid-cols-[minmax(12rem,15rem)_minmax(0,1fr)] items-start gap-4">
              <ProfileList mode="select" showOwnMark selectedId={shownId} onActivate={(p) => setSelectedId(p.id)} />
              <ProfileEditor key={shownId} profileId={shownId} />
            </div>
          ) : (
            <ProfileList mode="pick" showOwnMark onActivate={(p) => actions.pick(p.id)} onEdit={(p) => setSheetId(p.id)} />
          )}
        </CardContent>

        <CardFooter className="gap-1.5 px-4 text-xs text-muted-foreground sm:px-6">
          <Palette aria-hidden="true" className="size-3.5 shrink-0" />
          <span>
            Themes sync on their own →{" "}
            <Link to="/settings" search={{ tab: "themes" }} className="font-medium text-foreground underline underline-offset-2 hover:text-primary">
              Theme sync
            </Link>
          </span>
        </CardFooter>
      </Card>

      {!wide && (
        <Sheet open={!!sheetProfile} onOpenChange={(open) => !open && setSheetId(null)}>
          <SheetContent side="bottom" className="max-h-[90dvh] gap-0 overflow-y-auto pb-[calc(1rem+env(safe-area-inset-bottom))]" data-prototype-editor-sheet="">
            <SheetHeader className="pb-2">
              <SheetTitle>Edit profile</SheetTitle>
              <SheetDescription className="sr-only">Icon, name, switches, and delete for this profile.</SheetDescription>
            </SheetHeader>
            {sheetProfile && <ProfileEditor key={sheetProfile.id} profileId={sheetProfile.id} onDeleted={() => setSheetId(null)} className="mx-4" />}
          </SheetContent>
        </Sheet>
      )}
    </>
  )
}
