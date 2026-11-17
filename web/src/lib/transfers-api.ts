/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { apiRequest } from "@/lib/api"

export type TransferKind = "transfer" | "export"
export type TransferWay = "sftp" | "local" | "none" | "bittorrent"
export type TransferState = "waiting" | "copying" | "checking" | "finishing" | "done" | "failed" | "cancelled"
export type TransferAfter = "removeWithFiles" | "keepFiles" | "keepSeeding"
/** The way that the user picks in the Transfer dialog. */
export type TransferWayChoice = "auto" | "copy" | "bittorrent"
/** A filter chip of the Transfers page. "all" shows every row. */
export type TransferFilter = "all" | "active" | "waiting" | "finished" | "failed"

export interface TransferHint {
  kind: "tcpAndUtp"
  instanceId: number
}

export interface TransferProgress {
  done: number
  total: number
  /** Bytes a second over the last 10 seconds. */
  speed: number
}

export interface TransferRow {
  id: number
  kind: TransferKind
  /** Empty for an Export run, which copies nothing. */
  way: TransferWay | ""
  state: TransferState
  startedBy: "user" | "rule"
  ruleId: number | null
  /** Empty when no rule started the run, or the rule is gone. */
  ruleName: string
  sourceInstanceId: number
  targetInstanceId: number
  hash: string
  name: string
  /** The torrent of the request with its cross-seeds. */
  torrents: number
  after: TransferAfter
  leaveStopped: boolean
  /** Set while a failure waits for its retry. */
  retryAt: string | null
  failureClass: "" | "temporary" | "permanent"
  failureReason: string
  createdAt: string
  finishedAt: string | null
  hints: TransferHint[]
  /** Set only while the files pass. */
  progress: TransferProgress | null
}

export interface TransferCounts {
  active: number
  waiting: number
  finished: number
  failed: number
}

export interface TransferList {
  rows: TransferRow[]
  counts: TransferCounts
}

export interface TransferMember {
  hash: string
  name: string
  targetInstanceId: number
  savePath: string
  category: string
  tags: string[]
}

export interface TransferStep {
  name: string
  at: string
  detail: string
}

export interface TransferLogLine {
  at: string
  message: string
  hint?: TransferHint
}

export interface TransferDetail extends TransferRow {
  steps: TransferStep[]
  log: TransferLogLine[]
  members: TransferMember[]
}

/** The body of a start and of a summary. */
export interface TransferRequest {
  sourceInstanceId: number
  hash: string
  targetInstanceId: number
  savePath: string
  category: string
  tags: string[]
  after: TransferAfter
  leaveStopped: boolean
  way: TransferWayChoice
}

export interface TransferSummary {
  way: TransferWay
  bytes: number
  members: TransferMember[]
  free: number
  freeKnown: boolean
  noFileAccess: boolean
  hints: TransferHint[]
}

const json = (body: unknown): RequestInit => ({ method: "POST", body: JSON.stringify(body) })

export const transfersApi = {
  list: (filter: TransferFilter) =>
    apiRequest<TransferList>(filter === "all" ? "/transfers" : `/transfers?filter=${filter}`),
  get: (id: number) => apiRequest<TransferDetail>(`/transfers/${id}`),
  summary: (req: TransferRequest) => apiRequest<TransferSummary>("/transfers/summary", json(req)),
  start: (req: TransferRequest) => apiRequest<{ id: number }>("/transfers", json(req)),
  retry: (id: number) => apiRequest<{ id: number }>(`/transfers/${id}/retry`, { method: "POST" }),
  cancel: (id: number) => apiRequest<void>(`/transfers/${id}/cancel`, { method: "POST" }),
  remove: (id: number) => apiRequest<void>(`/transfers/${id}`, { method: "DELETE" }),
}
