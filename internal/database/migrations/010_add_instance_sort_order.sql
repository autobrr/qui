-- Add a sort_order column so users can control instance ordering across the UI
ALTER TABLE instances
ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

-- Initialize sort_order to preserve the current alphabetical ordering
WITH ordered AS (
    SELECT
        id,
        ROW_NUMBER() OVER (ORDER BY name COLLATE NOCASE, id) - 1 AS rn
    FROM instances
)
UPDATE instances
SET sort_order = (
    SELECT rn FROM ordered WHERE ordered.id = instances.id
);

-- Create an index to make ordered scans efficient
CREATE INDEX IF NOT EXISTS idx_instances_sort_order ON instances(sort_order, id);
