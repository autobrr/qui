-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- qui stores a large write in parts, each in its own transaction.
-- complete_reading_at is the time of the last reading that a write stored
-- with all its parts. It is NULL while the first write of the instance lacks
-- parts. A write before this migration stored all its rows at once.
ALTER TABLE transfer_history_instances ADD COLUMN complete_reading_at TIMESTAMP;
UPDATE transfer_history_instances SET complete_reading_at = last_reading_at;
