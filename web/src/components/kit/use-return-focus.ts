/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRef, type RefObject } from "react"

function openerOf(element: Element | null): HTMLElement | null {
  if (!(element instanceof HTMLElement)) return null
  const menu = element.closest<HTMLElement>("[role=menu]")
  if (!menu?.id) return element
  return document.querySelector<HTMLElement>(`[aria-controls="${CSS.escape(menu.id)}"]`) ?? element
}

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
