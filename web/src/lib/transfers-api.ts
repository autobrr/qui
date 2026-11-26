/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { apiRequest } from "@/lib/api"

export type TransferKind = "transfer" | "export"
export type TransferWay = "sftp" | "local" | "none" | "bittorrent"
export type TransferState = "waiting" | "copying" | "checking" | "finishing" | "done" | "failed" | "cancelled"
export type TransferAfter = "removeWithFiles" | "keepFiles" | "keepSeeding"
export type TransferWayChoice = "auto" | "copy" | "bittorrent"
export type TransferFilter = "all" | "active" | "waiting" | "finished" | "failed"
export type TransferBulkAction = "retry" | "cancel" | "remove"

export interface TransferHint {
  kind: "tcpAndUtp"
  instanceId: number
}

export interface TransferProgress {
  done: number
  total: number
  speed: number
}

export interface TransferRow {
  id: number
  kind: TransferKind
  way: TransferWay | ""
  state: TransferState
  startedBy: "user" | "rule"
  ruleId: number | null
  ruleName: string
  sourceInstanceId: number
  targetInstanceId: number
  hash: string
  name: string
  torrents: number
  size: number
  after: TransferAfter
  leaveStopped: boolean
  retryAt: string | null
  failureClass: "" | "temporary" | "permanent"
  failureReason: string
  createdAt: string
  finishedAt: string | null
  hints: TransferHint[]
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

export type TransferBulkRequest =
  | { action: TransferBulkAction; ids: number[] }
  | { action: Exclude<TransferBulkAction, "retry">; filter: TransferFilter }

export interface TransferBulkResult {
  done: number
  skipped: number
  failed: number
  reasons: string[]
}

export interface TransferSummary {
  way: TransferWay
  bytes: number
  members: TransferMember[]
  free: number
  freeKnown: boolean
  noFileAccess: boolean
  stayOnSource: number
  hints: TransferHint[]
}

export const transferPageSize = 50

export const transferListQuery = (filter: TransferFilter, page: number, pageSize = transferPageSize) => ({
  queryKey: ["transfers", "list", filter, page, pageSize],
  queryFn: () => transfersApi.list(filter, page, pageSize),
})

const json = (body: unknown): RequestInit => ({ method: "POST", body: JSON.stringify(body) })

export const transfersApi = {
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
