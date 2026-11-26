-- Add TLS skip verification option to qBittorrent instances
ALTER TABLE instances ADD COLUMN tls_skip_verify BOOLEAN NOT NULL DEFAULT 0;
