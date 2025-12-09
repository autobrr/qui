-- Fix lockout for users who had use_category_from_indexer enabled before migration 023.
-- Migration 023 added use_cross_category_suffix with DEFAULT TRUE, which caused both
-- settings to be enabled simultaneously. Since these settings are mutually exclusive
-- in the UI, users became locked out of both toggles.
--
-- Resolution: Preserve the user's original choice (use_category_from_indexer) and
-- disable the new setting that was auto-enabled by the migration default.

UPDATE cross_seed_settings
SET use_cross_category_suffix = 0
WHERE use_category_from_indexer = 1;
