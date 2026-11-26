ALTER TABLE dir_scan_directories
    ADD COLUMN allowed_download_clients TEXT NOT NULL DEFAULT '[]';
