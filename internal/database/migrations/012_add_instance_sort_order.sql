-- Add a sort_order column so users can control instance ordering across the UI
ALTER TABLE instances
ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

-- Initialize sort_order to preserve the current alphabetical ordering
WITH ordered AS (
    SELECT
        i.id,
        ROW_NUMBER() OVER (
            ORDER BY COALESCE(sp.value, '') COLLATE NOCASE, i.id
        ) - 1 AS rn
    FROM instances i
    LEFT JOIN string_pool sp ON i.name_id = sp.id
)
UPDATE instances
SET sort_order = (
    SELECT rn FROM ordered WHERE ordered.id = instances.id
);

-- Recreate the instances_view to expose sort_order alongside resolved string values
DROP VIEW IF EXISTS instances_view;
CREATE VIEW instances_view AS
SELECT 
    i.id,
    sp_name.value AS name,
    sp_host.value AS host,
    sp_username.value AS username,
    i.password_encrypted,
    sp_basic_username.value AS basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify,
    i.sort_order
FROM instances i
INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;

-- Create an index to make ordered scans efficient
CREATE INDEX IF NOT EXISTS idx_instances_sort_order ON instances(sort_order, id);
