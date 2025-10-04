-- Create sessions table for database-backed session storage
-- This replaces cookie-based sessions with secure database storage

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    expiry REAL NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions(expiry);