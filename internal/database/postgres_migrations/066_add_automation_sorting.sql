-- Add sorting_config column for configurable sorting of automations.
-- Stores JSON: {"type": "simple", "field": "SIZE", "direction": "DESC"} or {"type": "score", "scoreRules": [...]}
-- NULL means default (oldest first).
ALTER TABLE automations
    ADD COLUMN IF NOT EXISTS sorting_config TEXT;
