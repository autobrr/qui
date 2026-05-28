/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

/**
 * Build a Torrent object with sensible zero-ish defaults for unit tests.
 *
 * Only override the fields your test actually exercises — the rest carry
 * harmless placeholders so the test file isn't littered with irrelevant
 * boilerplate.
 */
export function makeTorrent(overrides: Partial<Torrent> = {}): Torrent {
  return {
    added_on: 0,
    amount_left: 0,
    auto_tmm: false,
    availability: 0,
    category: "",
    completed: 0,
    completion_on: 0,
    content_path: "",
    dl_limit: 0,
    dlspeed: 0,
    download_path: "",
    downloaded: 0,
    downloaded_session: 0,
    eta: 0,
    f_l_piece_prio: false,
    force_start: false,
    hash: "abcdef0123456789",
    infohash_v1: "",
    infohash_v2: "",
    popularity: 0,
    private: false,
    last_activity: 0,
    magnet_uri: "",
    max_ratio: 0,
    max_seeding_time: 0,
    name: "test-torrent",
    num_complete: 0,
    num_incomplete: 0,
    num_leechs: 0,
    num_seeds: 0,
    priority: 0,
    progress: 0,
    ratio: 0,
    ratio_limit: 0,
    reannounce: 0,
    save_path: "/downloads",
    seeding_time: 0,
    seeding_time_limit: 0,
    seen_complete: 0,
    seq_dl: false,
    size: 0,
    state: "downloading",
    super_seeding: false,
    tags: "",
    time_active: 0,
    total_size: 0,
    tracker: "",
    trackers_count: 0,
    up_limit: 0,
    uploaded: 0,
    uploaded_session: 0,
    upspeed: 0,
    ...overrides,
  }
}
