/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedGazelleLookupReason, CrossSeedGazelleLookupStatus } from "./crossseed"

export interface ProblemReportMatchPolicy {
  titleRescue: boolean
  individualEpisodes: boolean
  sizeTolerancePercent: number
  allowMissingCandidateGroup: boolean
  skipSizeCheck: boolean
  seasonPack: {
    skipRepackCompare: boolean
    simplifyHdrCompare: boolean
    simplifyWebCompare: boolean
    skipYearCompare: boolean
  }
}

export interface ProblemReportIndexer {
  id: number
  name: string
  reason?: string
}

export interface ProblemReportSearchScope {
  torznabIndexers: ProblemReportIndexer[]
  torznabSkipped: ProblemReportIndexer[]
  /** Why the search asks no Torznab indexer. */
  torznabSkipReason?: string
  gazelleSites: { site: string; status: CrossSeedGazelleLookupStatus; reason?: CrossSeedGazelleLookupReason | "disabled" | "no_api_key" }[]
}

export type ProblemReportStartedFrom = "menu" | "notice" | "search"

export type ProblemReportLogScope = "warnings" | "all" | "none"

export interface ProblemReportQuiFacts {
  version: string
  commit?: string
  startedAt: string
  newerRelease?: { version: string; url: string }
  os: string
  arch: string
  appContainer: boolean
  database: "sqlite" | "postgres"
  logLevel: string
  experimentalFeatures: string[]
}

export interface ProblemReportInstanceFacts {
  qbittorrentVersion?: string
  libtorrentVersion?: string
  state: "connected" | "offline" | "disabled"
  torrentCount?: number
}

export interface ProblemReportNoticeFacts {
  kind: string
  severity: string
  reason?: string
  cause: string
  openedAt: string
}

export interface ProblemReportFacts {
  qui: ProblemReportQuiFacts
  instances: ProblemReportInstanceFacts[]
  notices: ProblemReportNoticeFacts[]
  logs: { warnings: string[]; all: string[] }
  matchPolicy?: ProblemReportMatchPolicy
  searchScope?: ProblemReportSearchScope
}

export interface ProblemReportBrowserFacts {
  route: string
  userAgent: string
  viewportWidth: number
  viewportHeight: number
  devicePixelRatio: number
}

export interface ProblemReportPayload {
  submissionId: string
  text: string
  startedFrom: ProblemReportStartedFrom
  qui: ProblemReportQuiFacts
  instances: ProblemReportInstanceFacts[]
  notices: ProblemReportNoticeFacts[]
  browser: ProblemReportBrowserFacts
  failedRequests: unknown[]
  logs: { scope: ProblemReportLogScope; lines: string[] }
  search?: Record<string, unknown>
  matchPolicy?: ProblemReportMatchPolicy
  searchScope?: ProblemReportSearchScope
}

export type ProblemReportProvider = "github" | "discord"

export type ProblemReportSender =
  | { anonymous: true }
  | { anonymous: false; provider: ProblemReportProvider; name: string }

export type ProblemReportSignInState =
  | { state: "pending" | "done" | "expired" }
  | { state: "refused"; reason?: string }

export interface SentProblemReport {
  id: string
  submissionId: string
  title: string
  sentAt: string
  status: "received"
  /** The text after the server removed secrets from it. */
  text: string
}

/** Where a Problem report stands, as brrbin last answered. */
export type ProblemReportStatus = "received" | "issue" | "closed"

export interface ListedProblemReport {
  id: string
  title: string
  sentAt: string
  status: ProblemReportStatus
  /** Present while the status is issue. The URL is empty when brrbin sent no https URL. */
  issue?: { number: number; url: string; closed: boolean }
  /** Can be present while the status is closed. */
  reason?: string
  /** When qui last saw the status change. A new report has its sentAt. */
  statusChangedAt: string
}
