-- Add fingerprint column to licenses table for recovery when device-id file is lost
-- This provides a backup mechanism to prevent licenses from becoming invalid
-- when the fingerprint file is accidentally deleted or the machine ID changes
ALTER TABLE licenses ADD COLUMN fingerprint TEXT;

-- Index for potential lookups by fingerprint
CREATE INDEX idx_licenses_fingerprint ON licenses(fingerprint);
