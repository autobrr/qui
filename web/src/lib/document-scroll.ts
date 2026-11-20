/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

type ScrollWindow = Pick<Window, "scrollX" | "scrollY" | "scrollTo" | "addEventListener">

/**
 * Scrolls the document back to the top after each resize of the window, such as a rotation.
 * The app scrolls only inside its own containers, so a scrolled document is always wrong: an iOS Home Screen web app keeps the document scrolled by the status bar height after a rotation back to portrait, which moves the page under the status bar and puts the tap areas of fixed elements, such as MobileFooterNav, out of line with what shows.
 */
export function keepDocumentAtTop(win: ScrollWindow = window) {
  win.addEventListener("resize", () => {
    if (win.scrollX !== 0 || win.scrollY !== 0) win.scrollTo(0, 0)
  })
}
