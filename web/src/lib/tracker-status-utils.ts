/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent, TorrentTracker } from "@/types"

const DEFAULT_UNREGISTERED_STATUSES = [
  "complete season uploaded",
  "dead",
  "dupe",
  "i'm sorry dave, i can't do that",
  "infohash not found",
  "internal available",
  "not exist",
  "not registered",
  "nuked",
  "pack is available",
  "packs are available",
  "problem with description",
  "problem with file",
  "problem with pack",
  "retitled",
  "season pack",
  "specifically banned",
  "torrent does not exist",
  "torrent existiert nicht",
  "torrent has been deleted",
  "torrent has been nuked",
  "torrent is not authorized for use on this tracker",
  "torrent is not found",
  "torrent nicht gefunden",
  "tracker nicht registriert",
  "torrent not found",
  "trump",
  "unknown",
  "unregistered",
  "upgraded",
  "uploaded",
].map((entry) => entry.toLowerCase())

const TRACKER_DOWN_STATUSES = [
  "continue",
  "multiple choices",
  "not modified",
  "bad request",
  "unauthorized",
  "forbidden",
  "internal server error",
  "not implemented",
  "bad gateway",
  "service unavailable",
  "moved permanently",
  "moved temporarily",
  "(unknown http error)",
  "down",
  "maintenance",
  "tracker is down",
  "tracker unavailable",
  "truncated",
  "unreachable",
  "not working",
  "not responding",
  "timeout",
  "refused",
  "no connection",
  "cannot connect",
  "connection failed",
  "ssl error",
  "no data",
  "timed out",
  "temporarily disabled",
  "unresolvable",
  "host not found",
  "offline",
  "your request could not be processed, please try again later",
  "unable to process your request",
  "<none>",
].map((entry) => entry.toLowerCase())

function matchesTrackerMessage(message: string | undefined, candidates: string[]): boolean {
  if (!message) {
    return false
  }

  const normalized = message.trim().toLowerCase()
  if (!normalized) {
    return false
  }

  return candidates.some((candidate) => normalized.includes(candidate))
}

function trackerIsUnregistered(tracker: TorrentTracker): boolean {
  if (tracker.status !== 4) {
    return false
  }

  return matchesTrackerMessage(tracker.msg, DEFAULT_UNREGISTERED_STATUSES)
}

function trackerIsDown(tracker: TorrentTracker): boolean {
  if (tracker.status !== 4) {
    return false
  }

  return matchesTrackerMessage(tracker.msg, TRACKER_DOWN_STATUSES)
}

export type TrackerHealth = "unregistered" | "tracker_down" | null

export function getTrackerHealth(torrent: Torrent): TrackerHealth {
  if (!torrent.trackers || torrent.trackers.length === 0) {
    return null
  }

  for (const tracker of torrent.trackers) {
    if (trackerIsUnregistered(tracker)) {
      return "unregistered"
    }
  }

  for (const tracker of torrent.trackers) {
    if (trackerIsDown(tracker)) {
      return "tracker_down"
    }
  }

  return null
}

export const trackerStatusMessages = {
  unregistered: DEFAULT_UNREGISTERED_STATUSES,
  trackerDown: TRACKER_DOWN_STATUSES,
}
