-- we need to create a new table to change the name and add the polar_activation_id column
CREATE TABLE licenses
(
    id                  INTEGER PRIMARY KEY autoincrement,
    license_key         TEXT                      not null unique,
    product_name        TEXT                      not null,
    status              TEXT     DEFAULT 'active' not null,
    activated_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at          DATETIME,
    last_validated      DATETIME DEFAULT CURRENT_TIMESTAMP,
    polar_customer_id   TEXT,
    polar_product_id    TEXT,
    polar_activation_id TEXT,
    username            TEXT,
    created_at          DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE theme_licenses;

-- Index for performance
CREATE INDEX idx_licenses_status ON licenses(status);
CREATE INDEX idx_licenses_theme ON licenses(product_name);
CREATE INDEX idx_licenses_key ON licenses(license_key);
