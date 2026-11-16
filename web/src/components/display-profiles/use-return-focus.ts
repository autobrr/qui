/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRef, type RefObject } from "react"

/**
 * Focus return for a Radix dialog or sheet that opens with no Radix trigger, which Radix would send to the page body.
 * Call `remember` from `onOpenAutoFocus` and pass `restore` as `onCloseAutoFocus`. Focus goes back to `target` when
 * given, for an opener that a menu removes, else to the element that had focus when the dialog opened.
 */
export function useReturnFocus(target?: RefObject<HTMLElement | null>) {
  const opener = useRef<HTMLElement | null>(null)
  return {
    remember: () => {
      opener.current = target?.current ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null)
    },
    restore: (event: Event) => {
      event.preventDefault()
      if (opener.current?.isConnected) opener.current.focus()
    },
  }
}
