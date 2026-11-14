-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- One row for each System notice that the user dismissed and that stays
-- hidden after a restart, for example low_disk.
CREATE TABLE IF NOT EXISTS system_notice_dismissals (
    kind TEXT PRIMARY KEY
);
