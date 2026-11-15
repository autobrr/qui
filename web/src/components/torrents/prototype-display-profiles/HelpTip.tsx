/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): field help like FieldHelp (a muted icon, the app's
// tooltip look), but a tap or click pins it open and a second one closes it, so
// it works on phones and stays open while a long text is read. Hover and focus
// still open it.

import * as TooltipPrimitive from "@radix-ui/react-tooltip"
import { CircleQuestionMark, type LucideIcon } from "lucide-react"
import { useRef, useState, type ReactNode } from "react"

import { cn } from "@/lib/utils"

interface HelpTipProps {
  /** The accessible name of the trigger. */
  label: string
  icon?: LucideIcon
  iconClassName?: string
  className?: string
  children: ReactNode
}

export function HelpTip({ label, icon: Icon = CircleQuestionMark, iconClassName = "size-3.5", className, children }: HelpTipProps) {
  const [open, setOpen] = useState(false)
  // A click pins the tooltip open; a second click, Esc, a tap outside, or leaving by keyboard closes it.
  // Hover never pins it, and while it is pinned, moving the pointer away or scrolling keeps it open.
  const pinned = useRef(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const close = () => {
    pinned.current = false
    setOpen(false)
  }
  return (
    <TooltipPrimitive.Root open={open} onOpenChange={(next) => (next || !pinned.current) && setOpen(next)} delayDuration={200}>
      <TooltipPrimitive.Trigger asChild>
        <button
          ref={triggerRef}
          type="button"
          aria-label={label}
          data-prototype-help={label}
          // Radix closes a tooltip on pointer down and click; here they toggle it, and stay out of a row around it.
          onPointerDown={(e) => {
            e.preventDefault()
            e.stopPropagation()
          }}
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            pinned.current = !pinned.current
            setOpen(pinned.current)
          }}
          onBlur={close}
          className={cn(
            "inline-grid shrink-0 place-items-center rounded-full text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
            className
          )}
        >
          <Icon aria-hidden="true" className={iconClassName} />
        </button>
      </TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          onEscapeKeyDown={close}
          onPointerDownOutside={(e) => {
            // The trigger's own click decides; closing here first would reopen it.
            if (triggerRef.current?.contains(e.target as Node)) e.preventDefault()
            else close()
          }}
          sideOffset={4}
          collisionPadding={8}
          className="z-50 w-max max-w-[min(20rem,calc(100vw-1rem))] origin-(--radix-tooltip-content-transform-origin) rounded-md bg-primary px-3 py-2 text-xs leading-4 font-medium text-pretty text-primary-foreground animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95"
        >
          {children}
          <TooltipPrimitive.Arrow className="fill-primary" />
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  )
}

export function OwnSortAndFiltersHelp() {
  return (
    <div className="space-y-1.5">
      <p>When this is on, this profile keeps its own:</p>
      <ul className="list-disc space-y-0.5 pl-4">
        <li>sort order of the torrent list</li>
        <li>column filters and sidebar filters</li>
        <li>instances that the Unified view shows</li>
        <li>Group copies, on or off</li>
        <li>last open tab of the details panel</li>
        <li>instance that the Backups and RSS pages show</li>
      </ul>
      <p>
        A change to these in this profile does not reach other profiles. When this is off, this profile shares them with
        every other profile. Turning it off deletes this profile's own values.
      </p>
    </div>
  )
}

export const DEFAULT_SHARES_HELP =
  "Default always shares the sort, the filters, and the other view choices with every profile. To keep your own, make another profile and turn on Own sort and filters."
