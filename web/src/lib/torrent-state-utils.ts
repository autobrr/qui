/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"

// Human-friendly labels for qBittorrent torrent states
const getTorrentStateLabels = (): Record<string, string> => ({
  // Downloading related
  downloading: i18n.t("lib.torrentState.downloading"),
  metaDL: i18n.t("lib.torrentState.metaDL"),
  allocating: i18n.t("lib.torrentState.allocating"),
  stalledDL: i18n.t("lib.torrentState.stalledDL"),
  queuedDL: i18n.t("lib.torrentState.queuedDL"),
  checkingDL: i18n.t("lib.torrentState.checkingDL"),
  forcedDL: i18n.t("lib.torrentState.forcedDL"),

  // Uploading / Seeding related
  uploading: i18n.t("lib.torrentState.uploading"),
  stalledUP: i18n.t("lib.torrentState.stalledUP"),
  queuedUP: i18n.t("lib.torrentState.queuedUP"),
  checkingUP: i18n.t("lib.torrentState.checkingUP"),
  forcedUP: i18n.t("lib.torrentState.forcedUP"),

  // Paused / Stopped
  pausedDL: i18n.t("lib.torrentState.pausedDL"),
  pausedUP: i18n.t("lib.torrentState.pausedUP"),
  stoppedDL: i18n.t("lib.torrentState.stoppedDL"),
  stoppedUP: i18n.t("lib.torrentState.stoppedUP"),

  // Other
  error: i18n.t("lib.torrentState.error"),
  missingFiles: i18n.t("lib.torrentState.missingFiles"),
  checkingResumeData: i18n.t("lib.torrentState.checkingResumeData"),
  moving: i18n.t("lib.torrentState.moving"),
})

export function getStateLabel(state: string): string {
  return getTorrentStateLabels()[state] ?? state
}



