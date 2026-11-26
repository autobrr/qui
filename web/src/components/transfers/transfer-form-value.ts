/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferAfter, TransferRequest, TransferWayChoice } from "@/lib/transfers-api"

export interface TransferFormValue {
  targetInstanceId: number | null
  savePath: string
  category: string
  tags: string
  after: TransferAfter
  leaveStopped: boolean
  way: TransferWayChoice
}

export const emptyTransferForm: TransferFormValue = {
  targetInstanceId: null,
  savePath: "",
  category: "",
  tags: "",
  after: "removeWithFiles",
  leaveStopped: false,
  way: "auto",
}

export function transferRequestOf(value: TransferFormValue, sourceInstanceId: number, hash: string): TransferRequest | null {
  if (value.targetInstanceId === null) return null
  return {
    sourceInstanceId,
    hash,
    targetInstanceId: value.targetInstanceId,
    savePath: value.savePath.trim(),
    category: value.category,
    tags: value.tags.split(",").map(tag => tag.trim()).filter(Boolean),
    after: value.after,
    leaveStopped: value.leaveStopped,
    way: value.way,
  }
}
