-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE instance_reannounce_settings ADD COLUMN max_retries INTEGER NOT NULL DEFAULT 50;
