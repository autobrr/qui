-- Migrate from separate host and port fields to single URL field
-- This supports reverse proxy scenarios with paths (e.g., http://localhost:8080/qbittorrent/)

-- Add the new URL field
ALTER TABLE instances ADD COLUMN url TEXT;

-- Migrate existing data to URL format
UPDATE instances 
SET url = CASE 
    -- If host already has http or https, use as-is
    WHEN host LIKE 'http://%' OR host LIKE 'https://%' THEN host
    -- Otherwise build URL from host:port
    ELSE 'http://' || host || ':' || port
END;

-- Set default URL for any NULL values
UPDATE instances SET url = 'http://localhost:8080' WHERE url IS NULL OR url = '';

-- Drop old columns
ALTER TABLE instances DROP COLUMN host;
ALTER TABLE instances DROP COLUMN port;