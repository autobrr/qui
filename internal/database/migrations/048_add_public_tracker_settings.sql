-- Public tracker management settings (singleton table)
CREATE TABLE IF NOT EXISTS public_tracker_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    tracker_list_url TEXT NOT NULL DEFAULT '',
    cached_trackers TEXT NOT NULL DEFAULT '[]',
    last_fetched_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert default row
INSERT OR IGNORE INTO public_tracker_settings (id) VALUES (1);
