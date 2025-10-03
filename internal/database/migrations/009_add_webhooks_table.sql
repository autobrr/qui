CREATE TABLE webhooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL UNIQUE,
    api_key_id INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 0,
    autorun_enabled BOOLEAN NOT NULL DEFAULT 0,
    autorun_on_torrent_added_enabled BOOLEAN NOT NULL DEFAULT 0,
    qui_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE
);

-- Create index for faster lookups by instance_id
CREATE INDEX idx_webhooks_instance_id ON webhooks(instance_id);
