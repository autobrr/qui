-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Rewrites each time that the driver stored in the Go text form, for example
-- '2026-11-09 12:41:31.5 +0100 CET' or
-- '2026-11-09 11:41:31.947225572 +0000 UTC m=+12.5', to the form that the driver
-- writes now and that SQL time functions read: '2026-11-09 11:41:31.5+00:00'
-- (ADR 0045). A value in the database clock form or in the new form does not
-- match the GLOB pattern and stays as it is.
--
-- Every column of each table declared DATE, DATETIME, or TIMESTAMP gets the same
-- expression. datetime() subtracts the offset. The fraction digits stay as text.
-- The zone name and the monotonic suffix go away. If datetime() cannot read a
-- value, the value stays as it is.
--
-- The update triggers that set updated_at to the database clock are dropped
-- first and created again at the end, so that no updated_at changes.

DROP TRIGGER cross_seed_search_settings_updated_at;
DROP TRIGGER torznab_search_cache_settings_updated_at;
DROP TRIGGER trg_automations_updated;
DROP TRIGGER trg_cross_seed_settings_updated;
DROP TRIGGER trg_dashboard_settings_updated;
DROP TRIGGER trg_dir_scan_directories_updated;
DROP TRIGGER trg_dir_scan_settings_updated;
DROP TRIGGER trg_instance_crossseed_completion_settings_updated;
DROP TRIGGER trg_instance_reannounce_settings_updated;
DROP TRIGGER trg_log_exclusions_updated;
DROP TRIGGER trg_notification_targets_updated;
DROP TRIGGER trg_orphan_scan_settings_updated;
DROP TRIGGER trg_torznab_cooldowns_updated_at;
DROP TRIGGER trg_tracker_customizations_updated;
DROP TRIGGER update_arr_instances_updated_at;
DROP TRIGGER update_instance_backup_settings_updated_at;
DROP TRIGGER update_torznab_indexers_updated_at;
DROP TRIGGER update_user_updated_at;

UPDATE api_keys SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    last_used_at = CASE WHEN last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_used_at, 1, 19) || substr(last_used_at, 20 + instr(substr(last_used_at, 20), ' '), 3) || ':' || substr(last_used_at, 23 + instr(substr(last_used_at, 20), ' '), 2)) || substr(last_used_at, 20, instr(substr(last_used_at, 20), ' ') - 1) || '+00:00', last_used_at) ELSE last_used_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE arr_id_cache SET
    cached_at = CASE WHEN cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(cached_at, 1, 19) || substr(cached_at, 20 + instr(substr(cached_at, 20), ' '), 3) || ':' || substr(cached_at, 23 + instr(substr(cached_at, 20), ' '), 2)) || substr(cached_at, 20, instr(substr(cached_at, 20), ' ') - 1) || '+00:00', cached_at) ELSE cached_at END,
    expires_at = CASE WHEN expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(expires_at, 1, 19) || substr(expires_at, 20 + instr(substr(expires_at, 20), ' '), 3) || ':' || substr(expires_at, 23 + instr(substr(expires_at, 20), ' '), 2)) || substr(expires_at, 20, instr(substr(expires_at, 20), ' ') - 1) || '+00:00', expires_at) ELSE expires_at END
WHERE cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE arr_instances SET
    last_test_at = CASE WHEN last_test_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_test_at, 1, 19) || substr(last_test_at, 20 + instr(substr(last_test_at, 20), ' '), 3) || ':' || substr(last_test_at, 23 + instr(substr(last_test_at, 20), ' '), 2)) || substr(last_test_at, 20, instr(substr(last_test_at, 20), ' ') - 1) || '+00:00', last_test_at) ELSE last_test_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE last_test_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE automation_activity SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE automations SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE client_api_keys SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    last_used_at = CASE WHEN last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_used_at, 1, 19) || substr(last_used_at, 20 + instr(substr(last_used_at, 20), ' '), 3) || ':' || substr(last_used_at, 23 + instr(substr(last_used_at, 20), ' '), 2)) || substr(last_used_at, 20, instr(substr(last_used_at, 20), ' ') - 1) || '+00:00', last_used_at) ELSE last_used_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE client_settings SET
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_blocklist SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_feed_items SET
    first_seen_at = CASE WHEN first_seen_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(first_seen_at, 1, 19) || substr(first_seen_at, 20 + instr(substr(first_seen_at, 20), ' '), 3) || ':' || substr(first_seen_at, 23 + instr(substr(first_seen_at, 20), ' '), 2)) || substr(first_seen_at, 20, instr(substr(first_seen_at, 20), ' ') - 1) || '+00:00', first_seen_at) ELSE first_seen_at END,
    last_seen_at = CASE WHEN last_seen_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_seen_at, 1, 19) || substr(last_seen_at, 20 + instr(substr(last_seen_at, 20), ' '), 3) || ':' || substr(last_seen_at, 23 + instr(substr(last_seen_at, 20), ' '), 2)) || substr(last_seen_at, 20, instr(substr(last_seen_at, 20), ' ') - 1) || '+00:00', last_seen_at) ELSE last_seen_at END
WHERE first_seen_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_seen_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_partial_pool_member_files SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_partial_pool_members SET
    last_progress_at = CASE WHEN last_progress_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_progress_at, 1, 19) || substr(last_progress_at, 20 + instr(substr(last_progress_at, 20), ' '), 3) || ':' || substr(last_progress_at, 23 + instr(substr(last_progress_at, 20), ' '), 2)) || substr(last_progress_at, 20, instr(substr(last_progress_at, 20), ' ') - 1) || '+00:00', last_progress_at) ELSE last_progress_at END,
    retry_after = CASE WHEN retry_after GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(retry_after, 1, 19) || substr(retry_after, 20 + instr(substr(retry_after, 20), ' '), 3) || ':' || substr(retry_after, 23 + instr(substr(retry_after, 20), ' '), 2)) || substr(retry_after, 20, instr(substr(retry_after, 20), ' ') - 1) || '+00:00', retry_after) ELSE retry_after END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE last_progress_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR retry_after GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_partial_pools SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_runs SET
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_search_history SET
    last_searched_at = CASE WHEN last_searched_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_searched_at, 1, 19) || substr(last_searched_at, 20 + instr(substr(last_searched_at, 20), ' '), 3) || ':' || substr(last_searched_at, 23 + instr(substr(last_searched_at, 20), ' '), 2)) || substr(last_searched_at, 20, instr(substr(last_searched_at, 20), ' ') - 1) || '+00:00', last_searched_at) ELSE last_searched_at END
WHERE last_searched_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_search_history_indexers SET
    last_searched_at = CASE WHEN last_searched_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_searched_at, 1, 19) || substr(last_searched_at, 20 + instr(substr(last_searched_at, 20), ' '), 3) || ':' || substr(last_searched_at, 23 + instr(substr(last_searched_at, 20), ' '), 2)) || substr(last_searched_at, 20, instr(substr(last_searched_at, 20), ' ') - 1) || '+00:00', last_searched_at) ELSE last_searched_at END
WHERE last_searched_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_search_runs SET
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_search_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE cross_seed_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dashboard_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dir_scan_directories SET
    last_scan_at = CASE WHEN last_scan_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_scan_at, 1, 19) || substr(last_scan_at, 20 + instr(substr(last_scan_at, 20), ' '), 3) || ':' || substr(last_scan_at, 23 + instr(substr(last_scan_at, 20), ' '), 2)) || substr(last_scan_at, 20, instr(substr(last_scan_at, 20), ' ') - 1) || '+00:00', last_scan_at) ELSE last_scan_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE last_scan_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dir_scan_files SET
    file_mod_time = CASE WHEN file_mod_time GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(file_mod_time, 1, 19) || substr(file_mod_time, 20 + instr(substr(file_mod_time, 20), ' '), 3) || ':' || substr(file_mod_time, 23 + instr(substr(file_mod_time, 20), ' '), 2)) || substr(file_mod_time, 20, instr(substr(file_mod_time, 20), ' ') - 1) || '+00:00', file_mod_time) ELSE file_mod_time END,
    last_processed_at = CASE WHEN last_processed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_processed_at, 1, 19) || substr(last_processed_at, 20 + instr(substr(last_processed_at, 20), ' '), 3) || ':' || substr(last_processed_at, 23 + instr(substr(last_processed_at, 20), ' '), 2)) || substr(last_processed_at, 20, instr(substr(last_processed_at, 20), ' ') - 1) || '+00:00', last_processed_at) ELSE last_processed_at END
WHERE file_mod_time GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_processed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dir_scan_run_injections SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dir_scan_runs SET
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END
WHERE started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE dir_scan_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE disc_scan_runs SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE external_programs SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE filter_views SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_backup_items SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_backup_runs SET
    requested_at = CASE WHEN requested_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(requested_at, 1, 19) || substr(requested_at, 20 + instr(substr(requested_at, 20), ' '), 3) || ':' || substr(requested_at, 23 + instr(substr(requested_at, 20), ' '), 2)) || substr(requested_at, 20, instr(substr(requested_at, 20), ' ') - 1) || '+00:00', requested_at) ELSE requested_at END,
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END
WHERE requested_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_backup_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_crossseed_completion_settings SET
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_errors SET
    occurred_at = CASE WHEN occurred_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(occurred_at, 1, 19) || substr(occurred_at, 20 + instr(substr(occurred_at, 20), ' '), 3) || ':' || substr(occurred_at, 23 + instr(substr(occurred_at, 20), ' '), 2)) || substr(occurred_at, 20, instr(substr(occurred_at, 20), ' ') - 1) || '+00:00', occurred_at) ELSE occurred_at END
WHERE occurred_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE instance_reannounce_settings SET
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE licenses SET
    activated_at = CASE WHEN activated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(activated_at, 1, 19) || substr(activated_at, 20 + instr(substr(activated_at, 20), ' '), 3) || ':' || substr(activated_at, 23 + instr(substr(activated_at, 20), ' '), 2)) || substr(activated_at, 20, instr(substr(activated_at, 20), ' ') - 1) || '+00:00', activated_at) ELSE activated_at END,
    expires_at = CASE WHEN expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(expires_at, 1, 19) || substr(expires_at, 20 + instr(substr(expires_at, 20), ' '), 3) || ':' || substr(expires_at, 23 + instr(substr(expires_at, 20), ' '), 2)) || substr(expires_at, 20, instr(substr(expires_at, 20), ' ') - 1) || '+00:00', expires_at) ELSE expires_at END,
    last_validated = CASE WHEN last_validated GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_validated, 1, 19) || substr(last_validated, 20 + instr(substr(last_validated, 20), ' '), 3) || ':' || substr(last_validated, 23 + instr(substr(last_validated, 20), ' '), 2)) || substr(last_validated, 20, instr(substr(last_validated, 20), ' ') - 1) || '+00:00', last_validated) ELSE last_validated END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE activated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_validated GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE log_exclusions SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE media_id_cache SET
    cached_at = CASE WHEN cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(cached_at, 1, 19) || substr(cached_at, 20 + instr(substr(cached_at, 20), ' '), 3) || ':' || substr(cached_at, 23 + instr(substr(cached_at, 20), ' '), 2)) || substr(cached_at, 20, instr(substr(cached_at, 20), ' ') - 1) || '+00:00', cached_at) ELSE cached_at END
WHERE cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE notification_targets SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE orphan_scan_files SET
    modified_at = CASE WHEN modified_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(modified_at, 1, 19) || substr(modified_at, 20 + instr(substr(modified_at, 20), ' '), 3) || ':' || substr(modified_at, 23 + instr(substr(modified_at, 20), ' '), 2)) || substr(modified_at, 20, instr(substr(modified_at, 20), ' ') - 1) || '+00:00', modified_at) ELSE modified_at END
WHERE modified_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE orphan_scan_runs SET
    started_at = CASE WHEN started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(started_at, 1, 19) || substr(started_at, 20 + instr(substr(started_at, 20), ' '), 3) || ':' || substr(started_at, 23 + instr(substr(started_at, 20), ' '), 2)) || substr(started_at, 20, instr(substr(started_at, 20), ' ') - 1) || '+00:00', started_at) ELSE started_at END,
    completed_at = CASE WHEN completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(completed_at, 1, 19) || substr(completed_at, 20 + instr(substr(completed_at, 20), ' '), 3) || ':' || substr(completed_at, 23 + instr(substr(completed_at, 20), ' '), 2)) || substr(completed_at, 20, instr(substr(completed_at, 20), ' ') - 1) || '+00:00', completed_at) ELSE completed_at END
WHERE started_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR completed_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE orphan_scan_settings SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE season_pack_runs SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE string_pool SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE theme_settings SET
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torrent_files_cache SET
    cached_at = CASE WHEN cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(cached_at, 1, 19) || substr(cached_at, 20 + instr(substr(cached_at, 20), ' '), 3) || ':' || substr(cached_at, 23 + instr(substr(cached_at, 20), ' '), 2)) || substr(cached_at, 20, instr(substr(cached_at, 20), ' ') - 1) || '+00:00', cached_at) ELSE cached_at END
WHERE cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torrent_files_sync SET
    last_synced_at = CASE WHEN last_synced_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_synced_at, 1, 19) || substr(last_synced_at, 20 + instr(substr(last_synced_at, 20), ' '), 3) || ':' || substr(last_synced_at, 23 + instr(substr(last_synced_at, 20), ' '), 2)) || substr(last_synced_at, 20, instr(substr(last_synced_at, 20), ' ') - 1) || '+00:00', last_synced_at) ELSE last_synced_at END,
    print_synced_at = CASE WHEN print_synced_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(print_synced_at, 1, 19) || substr(print_synced_at, 20 + instr(substr(print_synced_at, 20), ' '), 3) || ':' || substr(print_synced_at, 23 + instr(substr(print_synced_at, 20), ' '), 2)) || substr(print_synced_at, 20, instr(substr(print_synced_at, 20), ' ') - 1) || '+00:00', print_synced_at) ELSE print_synced_at END
WHERE last_synced_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR print_synced_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexer_capabilities SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexer_categories SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexer_cooldowns SET
    resume_at = CASE WHEN resume_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(resume_at, 1, 19) || substr(resume_at, 20 + instr(substr(resume_at, 20), ' '), 3) || ':' || substr(resume_at, 23 + instr(substr(resume_at, 20), ' '), 2)) || substr(resume_at, 20, instr(substr(resume_at, 20), ' ') - 1) || '+00:00', resume_at) ELSE resume_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE resume_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexer_errors SET
    occurred_at = CASE WHEN occurred_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(occurred_at, 1, 19) || substr(occurred_at, 20 + instr(substr(occurred_at, 20), ' '), 3) || ':' || substr(occurred_at, 23 + instr(substr(occurred_at, 20), ' '), 2)) || substr(occurred_at, 20, instr(substr(occurred_at, 20), ' ') - 1) || '+00:00', occurred_at) ELSE occurred_at END,
    resolved_at = CASE WHEN resolved_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(resolved_at, 1, 19) || substr(resolved_at, 20 + instr(substr(resolved_at, 20), ' '), 3) || ':' || substr(resolved_at, 23 + instr(substr(resolved_at, 20), ' '), 2)) || substr(resolved_at, 20, instr(substr(resolved_at, 20), ' ') - 1) || '+00:00', resolved_at) ELSE resolved_at END
WHERE occurred_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR resolved_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexer_latency SET
    measured_at = CASE WHEN measured_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(measured_at, 1, 19) || substr(measured_at, 20 + instr(substr(measured_at, 20), ' '), 3) || ':' || substr(measured_at, 23 + instr(substr(measured_at, 20), ' '), 2)) || substr(measured_at, 20, instr(substr(measured_at, 20), ' ') - 1) || '+00:00', measured_at) ELSE measured_at END
WHERE measured_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_indexers SET
    last_test_at = CASE WHEN last_test_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_test_at, 1, 19) || substr(last_test_at, 20 + instr(substr(last_test_at, 20), ' '), 3) || ':' || substr(last_test_at, 23 + instr(substr(last_test_at, 20), ' '), 2)) || substr(last_test_at, 20, instr(substr(last_test_at, 20), ' ') - 1) || '+00:00', last_test_at) ELSE last_test_at END,
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE last_test_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_search_cache SET
    cached_at = CASE WHEN cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(cached_at, 1, 19) || substr(cached_at, 20 + instr(substr(cached_at, 20), ' '), 3) || ':' || substr(cached_at, 23 + instr(substr(cached_at, 20), ' '), 2)) || substr(cached_at, 20, instr(substr(cached_at, 20), ' ') - 1) || '+00:00', cached_at) ELSE cached_at END,
    last_used_at = CASE WHEN last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_used_at, 1, 19) || substr(last_used_at, 20 + instr(substr(last_used_at, 20), ' '), 3) || ':' || substr(last_used_at, 23 + instr(substr(last_used_at, 20), ' '), 2)) || substr(last_used_at, 20, instr(substr(last_used_at, 20), ' ') - 1) || '+00:00', last_used_at) ELSE last_used_at END,
    expires_at = CASE WHEN expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(expires_at, 1, 19) || substr(expires_at, 20 + instr(substr(expires_at, 20), ' '), 3) || ':' || substr(expires_at, 23 + instr(substr(expires_at, 20), ' '), 2)) || substr(expires_at, 20, instr(substr(expires_at, 20), ' ') - 1) || '+00:00', expires_at) ELSE expires_at END
WHERE cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR expires_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_search_cache_settings SET
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE torznab_torrent_cache SET
    cached_at = CASE WHEN cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(cached_at, 1, 19) || substr(cached_at, 20 + instr(substr(cached_at, 20), ' '), 3) || ':' || substr(cached_at, 23 + instr(substr(cached_at, 20), ' '), 2)) || substr(cached_at, 20, instr(substr(cached_at, 20), ' ') - 1) || '+00:00', cached_at) ELSE cached_at END,
    last_used_at = CASE WHEN last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(last_used_at, 1, 19) || substr(last_used_at, 20 + instr(substr(last_used_at, 20), ' '), 3) || ':' || substr(last_used_at, 23 + instr(substr(last_used_at, 20), ' '), 2)) || substr(last_used_at, 20, instr(substr(last_used_at, 20), ' ') - 1) || '+00:00', last_used_at) ELSE last_used_at END
WHERE cached_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR last_used_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE tracker_customizations SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

UPDATE "user" SET
    created_at = CASE WHEN created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(created_at, 1, 19) || substr(created_at, 20 + instr(substr(created_at, 20), ' '), 3) || ':' || substr(created_at, 23 + instr(substr(created_at, 20), ' '), 2)) || substr(created_at, 20, instr(substr(created_at, 20), ' ') - 1) || '+00:00', created_at) ELSE created_at END,
    updated_at = CASE WHEN updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *' THEN COALESCE(datetime(substr(updated_at, 1, 19) || substr(updated_at, 20 + instr(substr(updated_at, 20), ' '), 3) || ':' || substr(updated_at, 23 + instr(substr(updated_at, 20), ' '), 2)) || substr(updated_at, 20, instr(substr(updated_at, 20), ' ') - 1) || '+00:00', updated_at) ELSE updated_at END
WHERE created_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *'
   OR updated_at GLOB '????-??-?? ??:??:??* [-+][0-9][0-9][0-9][0-9] *';

CREATE TRIGGER cross_seed_search_settings_updated_at
AFTER UPDATE ON cross_seed_search_settings
FOR EACH ROW
BEGIN
    UPDATE cross_seed_search_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

CREATE TRIGGER torznab_search_cache_settings_updated_at
AFTER UPDATE ON torznab_search_cache_settings
FOR EACH ROW
BEGIN
    UPDATE torznab_search_cache_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

CREATE TRIGGER trg_automations_updated
AFTER UPDATE ON automations
BEGIN
    UPDATE automations
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

CREATE TRIGGER trg_cross_seed_settings_updated
AFTER UPDATE ON cross_seed_settings
BEGIN
    UPDATE cross_seed_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

CREATE TRIGGER trg_dashboard_settings_updated
AFTER UPDATE ON dashboard_settings
BEGIN
    UPDATE dashboard_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_dir_scan_directories_updated
AFTER UPDATE ON dir_scan_directories
BEGIN
    UPDATE dir_scan_directories SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_dir_scan_settings_updated
AFTER UPDATE ON dir_scan_settings
BEGIN
    UPDATE dir_scan_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_instance_crossseed_completion_settings_updated
AFTER UPDATE ON instance_crossseed_completion_settings
BEGIN
    UPDATE instance_crossseed_completion_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;

CREATE TRIGGER trg_instance_reannounce_settings_updated
AFTER UPDATE ON instance_reannounce_settings
BEGIN
    UPDATE instance_reannounce_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;

CREATE TRIGGER trg_log_exclusions_updated
AFTER UPDATE ON log_exclusions
BEGIN
    UPDATE log_exclusions SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_notification_targets_updated
AFTER UPDATE ON notification_targets
BEGIN
    UPDATE notification_targets SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_orphan_scan_settings_updated
AFTER UPDATE ON orphan_scan_settings
BEGIN
    UPDATE orphan_scan_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER trg_torznab_cooldowns_updated_at
AFTER UPDATE ON torznab_indexer_cooldowns
FOR EACH ROW
BEGIN
    UPDATE torznab_indexer_cooldowns
    SET updated_at = CURRENT_TIMESTAMP
    WHERE indexer_id = NEW.indexer_id;
END;

CREATE TRIGGER trg_tracker_customizations_updated
AFTER UPDATE ON tracker_customizations
BEGIN
    UPDATE tracker_customizations SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER update_arr_instances_updated_at
AFTER UPDATE ON arr_instances
BEGIN
    UPDATE arr_instances SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER update_instance_backup_settings_updated_at
AFTER UPDATE ON instance_backup_settings
BEGIN
    UPDATE instance_backup_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;

CREATE TRIGGER update_torznab_indexers_updated_at
AFTER UPDATE ON torznab_indexers
BEGIN
    UPDATE torznab_indexers SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER update_user_updated_at 
AFTER UPDATE ON user
BEGIN
    UPDATE user SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
