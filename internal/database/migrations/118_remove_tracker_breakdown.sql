-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The Tracker Breakdown card left the Dashboard. Statistics counts a torrent
-- toward one Tracker group, so the "counts toward statistics" domains of a
-- tracker customization have no reader.
ALTER TABLE tracker_customizations DROP COLUMN included_in_stats;

ALTER TABLE dashboard_settings DROP COLUMN tracker_breakdown_sort_column;
ALTER TABLE dashboard_settings DROP COLUMN tracker_breakdown_sort_direction;
ALTER TABLE dashboard_settings DROP COLUMN tracker_breakdown_items_per_page;

UPDATE dashboard_settings
SET section_order = (
    SELECT json_group_array(value ORDER BY key)
    FROM json_each(dashboard_settings.section_order)
    WHERE value <> 'tracker-breakdown'
)
WHERE json_valid(section_order) AND section_order LIKE '%tracker-breakdown%';

UPDATE dashboard_settings
SET section_visibility = json_remove(section_visibility, '$."tracker-breakdown"')
WHERE json_valid(section_visibility) AND section_visibility LIKE '%tracker-breakdown%';

UPDATE dashboard_settings
SET section_collapsed = json_remove(section_collapsed, '$."tracker-breakdown"')
WHERE json_valid(section_collapsed) AND section_collapsed LIKE '%tracker-breakdown%';
