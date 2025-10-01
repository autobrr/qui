-- Add is_webhook column to client_api_keys table
ALTER TABLE client_api_keys ADD COLUMN is_webhook BOOLEAN NOT NULL DEFAULT false;

-- Create index for faster lookups by instance_id and is_webhook
CREATE INDEX idx_client_api_keys_instance_webhook ON client_api_keys(instance_id, is_webhook);

