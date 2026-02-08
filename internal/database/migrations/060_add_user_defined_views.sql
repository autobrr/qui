-- Create user-defined views
CREATE TABLE IF NOT EXISTS user_defined_views (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT '[]',
    categories TEXT NOT NULL DEFAULT '[]',
    tags TEXT NOT NULL DEFAULT '[]',
    trackers TEXT NOT NULL DEFAULT '[]',
    exclude_status TEXT NOT NULL DEFAULT '[]',
    exclude_categories TEXT NOT NULL DEFAULT '[]',
    exclude_tags TEXT NOT NULL DEFAULT '[]',
    exclude_trackers TEXT NOT NULL DEFAULT '[]',
    UNIQUE(instance_id, name)
);