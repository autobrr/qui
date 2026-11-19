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
/** A step that the Transfers page takes on many rows at once. */
export type TransferBulkAction = "retry" | "cancel" | "remove"

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
  /** The size of the torrent of the request at the start, in bytes. 0 when unknown. */
  size: number
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

/**
 * A bulk step names the rows by id, or picks every row behind a filter chip, on every page. A retry plans each
 * Transfer again, so it takes ids only.
 */
export type TransferBulkRequest =
  | { action: TransferBulkAction; ids: number[] }
  | { action: Exclude<TransferBulkAction, "retry">; filter: TransferFilter }

export interface TransferBulkResult {
  /** The rows that took the step. */
  done: number
  /** The rows that the step does not fit, for example a cancel of a row that ended. */
  skipped: number
  /** The rows where qui refused the step or an error stopped it. */
  failed: number
  /** Each reason that qui gave for a refusal, once. */
  reasons: string[]
}

export interface TransferSummary {
  way: TransferWay
  bytes: number
  members: TransferMember[]
  free: number
  freeKnown: boolean
  noFileAccess: boolean
  /** The cross-seeds on hardlinks that stay on the source, because a Transfer torrent makes no hardlinks. */
  stayOnSource: number
  hints: TransferHint[]
}

/** The number of rows on a page of the Transfers list. */
export const transferPageSize = 50

/** The query of one page of the Transfers list. The Transfers page and the badge of the sidebar share it. */
export const transferListQuery = (filter: TransferFilter, page: number, pageSize = transferPageSize) => ({
  queryKey: ["transfers", "list", filter, page, pageSize],
  queryFn: () => transfersApi.list(filter, page, pageSize),
})

const json = (body: unknown): RequestInit => ({ method: "POST", body: JSON.stringify(body) })

export const transfersApi = {
  /** One page of the rows behind filter, the newest first. Page 0 is the first page. */
  list: (filter: TransferFilter, page: number, pageSize = transferPageSize) => {
    const query = new URLSearchParams({ limit: String(pageSize), offset: String(page * pageSize) })
    if (filter !== "all") query.set("filter", filter)
    return apiRequest<TransferList>(`/transfers?${query}`)
  },
  get: (id: number) => apiRequest<TransferDetail>(`/transfers/${id}`),
  summary: (req: TransferRequest) => apiRequest<TransferSummary>("/transfers/summary", json(req)),
  start: (req: TransferRequest) => apiRequest<{ id: number }>("/transfers", json(req)),
  retry: (id: number) => apiRequest<{ id: number }>(`/transfers/${id}/retry`, { method: "POST" }),
  cancel: (id: number) => apiRequest<void>(`/transfers/${id}/cancel`, { method: "POST" }),
  remove: (id: number) => apiRequest<void>(`/transfers/${id}`, { method: "DELETE" }),
  bulk: (req: TransferBulkRequest) => apiRequest<TransferBulkResult>("/transfers/bulk", json(req)),
}
