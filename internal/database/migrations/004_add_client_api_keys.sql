-- Create client_api_keys table for proxy authentication
CREATE TABLE client_api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash TEXT NOT NULL UNIQUE,
    client_name TEXT NOT NULL,
    instance_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

-- Create index for faster lookups by key_hash
CREATE INDEX idx_client_api_keys_key_hash ON client_api_keys(key_hash);

-- Create index for instance_id lookups
CREATE INDEX idx_client_api_keys_instance_id ON client_api_keys(instance_id);