/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { Link } from "@tanstack/react-router"
import { Check, ChevronUp, Plus, Settings2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { markFeatureNoticeSeen } from "@/components/feature-notice/due-notice"
import { FeatureNotice } from "@/components/feature-notice/FeatureNotice"
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
import { TruncatedText } from "@/components/ui/truncated-text"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"

import { useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileGlyph, DisplayProfileTile } from "./DisplayProfileTile"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

export function DisplayProfileChip() {
  const { t } = useTranslation("torrents")
  const { profiles, active, settled, pick } = useNamedDisplayProfiles()
  const wide = useMediaQuery("(min-width: 1024px)")
  const [menuOpen, setMenuOpen] = useState(false)
  const [newProfileOpen, setNewProfileOpen] = useState(false)
  const activeRowRef = useRef<HTMLDivElement>(null)
  const chipRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!menuOpen) return
    const frame = requestAnimationFrame(() => activeRowRef.current?.focus())
    return () => cancelAnimationFrame(frame)
  }, [menuOpen])

  return (
    <>
      <DropdownMenu
        open={menuOpen}
        onOpenChange={(open) => {
          setMenuOpen(open)
          if (open) markFeatureNoticeSeen("display-profiles")
        }}
      >
        <Tooltip disableHoverableContent>
          <FeatureNotice id="display-profiles" side="top" onAction={() => setNewProfileOpen(true)}>
            <TooltipTrigger asChild onFocus={(event) => event.preventDefault()}>
              <DropdownMenuTrigger asChild>
                <Button
                  ref={chipRef}
                  variant="ghost"
                  size="sm"
                  aria-label={t("displayProfiles.chipAriaLabel", { name: active.name })}
                  className={cn(
                    "hidden h-6 gap-1 rounded-full border px-1.5 text-xs md:inline-flex lg:px-2",
                    menuOpen ? "bg-accent text-foreground" : "text-muted-foreground hover:text-accent-foreground"
                  )}
                >
                  <DisplayProfileGlyph profile={active} />
                  <span className="hidden max-w-[8rem] truncate lg:inline">{active.name}</span>
                  <ChevronUp aria-hidden="true" className="hidden size-3 opacity-60 lg:block" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
          </FeatureNotice>
          {!wide && !menuOpen && <TooltipContent>{active.name}</TooltipContent>}
        </Tooltip>
        <DropdownMenuContent
          side="top"
          align="end"
          sideOffset={8}
          className="w-64 max-w-[calc(100vw-1rem)]"
        >
          <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t("displayProfiles.menuLabel")}</DropdownMenuLabel>
          <DropdownMenuPrimitive.RadioGroup
            value={active.id}
            onValueChange={pick}
            className="max-h-[min(18rem,calc(var(--radix-dropdown-menu-content-available-height)-6rem))] overflow-y-auto"
          >
            {profiles.map((profile) => (
              <DropdownMenuPrimitive.RadioItem
                key={profile.id}
                ref={profile.id === active.id ? activeRowRef : undefined}
                value={profile.id}
                textValue={profile.name}
                disabled={!settled}
                className="flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:opacity-50 data-[state=checked]:font-medium"
              >
                <DisplayProfileTile profile={profile} size="sm" />
                <TruncatedText className="min-w-0 flex-1" openOnTap={false}>{profile.name}</TruncatedText>
                <DropdownMenuPrimitive.ItemIndicator className="flex size-4 shrink-0 items-center justify-center">
                  <Check aria-hidden="true" className="size-4" />
                </DropdownMenuPrimitive.ItemIndicator>
              </DropdownMenuPrimitive.RadioItem>
            ))}
          </DropdownMenuPrimitive.RadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={!settled} onSelect={() => setNewProfileOpen(true)}>
            <Plus />
            {t("displayProfiles.newProfile")}
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/settings" search={{ tab: "display-profiles" }}>
              <Settings2 />
              {t("displayProfiles.manage")}
            </Link>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <NewDisplayProfileDialog open={newProfileOpen} onOpenChange={setNewProfileOpen} returnFocusRef={chipRef} />
    </>
  )
}
