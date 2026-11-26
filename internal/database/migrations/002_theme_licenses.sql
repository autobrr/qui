-- Add theme licenses table for Polar SDK integration
CREATE TABLE theme_licenses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    license_key TEXT UNIQUE NOT NULL,
    theme_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active', -- 'active', 'expired', 'invalid'
    activated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    last_validated DATETIME DEFAULT CURRENT_TIMESTAMP,
    polar_customer_id TEXT,
    polar_product_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Index for performance
CREATE INDEX idx_theme_licenses_status ON theme_licenses(status);
CREATE INDEX idx_theme_licenses_theme ON theme_licenses(theme_name);
CREATE INDEX idx_theme_licenses_key ON theme_licenses(license_key);