/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRef, type RefObject } from "react"

/** The element that opened a dialog. A menu item goes away with its menu, so the button that opened the menu takes its place. */
function openerOf(element: Element | null): HTMLElement | null {
  if (!(element instanceof HTMLElement)) return null
  const menu = element.closest<HTMLElement>("[role=menu]")
  if (!menu?.id) return element
  return document.querySelector<HTMLElement>(`[aria-controls="${CSS.escape(menu.id)}"]`) ?? element
}

/**
 * Focus return for a Radix dialog or sheet that opens with no Radix trigger, which Radix would send to the page body.
 * Call `remember` from `onOpenAutoFocus` and pass `restore` as `onCloseAutoFocus`. Focus goes back to `target` when
 * given, else to the element that had focus when the dialog opened. For an item of a menu, that is the menu button.
 */
export function useReturnFocus(target?: RefObject<HTMLElement | null>) {
  const opener = useRef<HTMLElement | null>(null)
  return {
    remember: () => {
      opener.current = target?.current ?? openerOf(document.activeElement)
    },
    restore: (event: Event) => {
      event.preventDefault()
      if (opener.current?.isConnected) opener.current.focus()
    },
  }
}
