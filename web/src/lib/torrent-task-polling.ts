/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentCreationTask } from "@/types"

export const ACTIVE_TORRENT_TASK_POLL_INTERVAL = 5000
export const IDLE_TORRENT_TASK_POLL_INTERVAL = 30000

type PollingOptions = {
  activeInterval?: number
  idleInterval?: number
}

export function getTorrentTaskPollInterval(
  tasks: TorrentCreationTask[] | undefined,
  options: PollingOptions = {}
): number {
  const { activeInterval = ACTIVE_TORRENT_TASK_POLL_INTERVAL, idleInterval = IDLE_TORRENT_TASK_POLL_INTERVAL } = options

  if (tasks?.some((task) => task.status === "Running" || task.status === "Queued")) {
    return activeInterval
  }

  return idleInterval
}
