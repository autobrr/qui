-- Create view for instances that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS instances_view AS
SELECT 
    i.id,
    sp.value AS name,
    i.host,
    i.username,
    i.password_encrypted,
    i.basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify
FROM instances i
INNER JOIN string_pool sp ON i.name_id = sp.id;
