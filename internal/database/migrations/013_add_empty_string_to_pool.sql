-- Migration 013: Add empty string to string_pool for localhost bypass support
-- This fixes issue #573 where localhost bypass authentication fails in v1.7.0+
-- The empty string is needed when creating instances with empty username (localhost bypass)

INSERT OR IGNORE INTO string_pool (value) VALUES ('');
