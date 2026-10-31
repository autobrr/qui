-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The instance that receives a season pack add: the Cross-seed instance of a
-- routed apply (ADR 0035), or the instance in instance_id.
ALTER TABLE season_pack_runs ADD COLUMN added_instance_id INTEGER;
