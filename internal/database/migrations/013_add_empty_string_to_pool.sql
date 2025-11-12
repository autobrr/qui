-- Migration 013: Add empty string to string_pool for localhost bypass support
-- This fixes issue #573 where localhost bypass authentication fails in v1.7.0+
-- The empty string is needed when creating instances with empty username (localhost bypass)
--
-- NOTE: This migration compensates for a retroactive change made to migration 010 after
-- its release (commit 9480692). Migration 010 originally inserted only '(unknown)' and
-- '(unnamed)' but was later modified to include '' which broke migration immutability.
-- Migration 010 has been reverted to its original form, and this migration provides the
-- forward-safe fix for already-deployed databases.

INSERT OR IGNORE INTO string_pool (value) VALUES ('');
