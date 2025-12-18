-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add conditions column for storing JSON query builder conditions
ALTER TABLE tracker_rules ADD COLUMN conditions TEXT;
