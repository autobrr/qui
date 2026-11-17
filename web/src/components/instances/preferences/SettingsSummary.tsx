/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Fragment } from "react"

// Joins the pairs in code so no translation can change the separator, and keeps each pair with its "·" on one line.
export function SettingsSummary({ parts }: { parts: string[] }) {
  return parts.map((part, i) => (
    <Fragment key={i}>
      <span className="whitespace-nowrap">{i < parts.length - 1 ? `${part}\u00a0·` : part}</span>
      {i < parts.length - 1 && " "}
    </Fragment>
  ))
}
