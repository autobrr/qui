-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE instance_reannounce_settings
    ADD COLUMN health_focus_trackers_json TEXT NOT NULL DEFAULT '[]';

-- Seed a sane default ignore list to avoid perma-dead public trackers causing
-- infinite retries and noise by default.
ALTER TABLE instance_reannounce_settings
    ADD COLUMN health_ignore_trackers_json TEXT NOT NULL DEFAULT '["sptracker.cc"]';
