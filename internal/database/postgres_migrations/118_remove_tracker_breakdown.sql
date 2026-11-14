-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The Tracker Breakdown card left the Dashboard. Statistics counts a torrent
-- toward one Tracker group, so the "counts toward statistics" domains of a
-- tracker customization have no reader.
ALTER TABLE tracker_customizations DROP COLUMN included_in_stats;

ALTER TABLE dashboard_settings
    DROP COLUMN tracker_breakdown_sort_column,
    DROP COLUMN tracker_breakdown_sort_direction,
    DROP COLUMN tracker_breakdown_items_per_page;

-- The jsonb minus operator removes a string from an array and a key from an
-- object.
UPDATE dashboard_settings
SET section_order = (section_order::jsonb - 'tracker-breakdown')::text
WHERE section_order LIKE '%tracker-breakdown%';

UPDATE dashboard_settings
SET section_visibility = (section_visibility::jsonb - 'tracker-breakdown')::text
WHERE section_visibility LIKE '%tracker-breakdown%';

UPDATE dashboard_settings
SET section_collapsed = (section_collapsed::jsonb - 'tracker-breakdown')::text
WHERE section_collapsed LIKE '%tracker-breakdown%';
