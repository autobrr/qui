-- Track whether an instance should be actively polled or remains disabled
ALTER TABLE instances
ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT 1;

-- Ensure all existing rows are marked active to preserve current behaviour
UPDATE instances
SET is_active = 1
WHERE is_active IS NULL;

-- Recreate the instances_view so it exposes the activation flag
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
    i.sort_order,
    i.is_active
FROM instances i
INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;

-- Keep lookups fast when filtering by activation state
CREATE INDEX IF NOT EXISTS idx_instances_is_active ON instances(is_active, id);
