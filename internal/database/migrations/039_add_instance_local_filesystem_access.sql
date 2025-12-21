-- Track whether qui has local filesystem access to this instance for hardlink detection
-- Default to false for existing instances (opt-in); new instances via UI also default to false
ALTER TABLE instances
ADD COLUMN has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0;

-- Recreate the instances_view so it exposes the new flag
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
    i.is_active,
    i.has_local_filesystem_access
FROM instances i
INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
