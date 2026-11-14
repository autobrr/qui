-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- One row for each Source instance that names a Cross-seed instance (ADR 0035).
-- rows_json holds the mapped path of each base folder of the Source instance.
CREATE TABLE IF NOT EXISTS cross_seed_routes (
    source_instance_id INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    cross_seed_instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    rows_json TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_routes_cross_seed_instance ON cross_seed_routes(cross_seed_instance_id);
