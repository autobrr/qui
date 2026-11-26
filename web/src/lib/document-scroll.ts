/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

type ScrollWindow = Pick<Window, "scrollX" | "scrollY" | "scrollTo" | "addEventListener">

export function keepDocumentAtTop(win: ScrollWindow = window) {
  win.addEventListener("resize", () => {
    if (win.scrollX !== 0 || win.scrollY !== 0) win.scrollTo(0, 0)
  })
}
