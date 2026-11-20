/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedGazelleLookupReason, CrossSeedGazelleLookupStatus } from "./crossseed"

/** The rules that the cross-seed search matched with. */
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
  /** Why the search skips the indexer. */
  reason?: string
}

/** The Torznab indexers and RED/OPS sites that the search asks or skips. */
export interface ProblemReportSearchScope {
  torznabIndexers: ProblemReportIndexer[]
  torznabSkipped: ProblemReportIndexer[]
  /** Why the search asks no Torznab indexer. */
  torznabSkipReason?: string
  gazelleSites: { site: string; status: CrossSeedGazelleLookupStatus; reason?: CrossSeedGazelleLookupReason }[]
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

/** The server facts that GET /api/problem-reports/facts returns for a starting point. */
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

/** The `report` part of POST /api/problem-reports. */
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

/** One row of Sent reports, as GET /api/problem-reports returns it. */
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
