ALTER TABLE licenses ADD COLUMN provider TEXT;
ALTER TABLE licenses ADD COLUMN dodo_instance_id TEXT;
UPDATE licenses
SET provider = 'polar'
WHERE provider IS NULL OR provider = '';
