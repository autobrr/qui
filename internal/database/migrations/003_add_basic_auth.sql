-- Add HTTP Basic Authentication fields to instances table
ALTER TABLE instances ADD COLUMN basic_username TEXT;
ALTER TABLE instances ADD COLUMN basic_password_encrypted TEXT;