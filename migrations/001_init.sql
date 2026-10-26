CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    message TEXT NOT NULL,
    author TEXT NOT NULL,
    can_comment BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    parent_id TEXT REFERENCES comments(id) ON DELETE CASCADE,
    message TEXT NOT NULL CHECK (char_length(message) <= 2000),
    author TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_comments_post_id
    ON comments(post_id);

CREATE INDEX IF NOT EXISTS idx_comments_parent_id
    ON comments(parent_id);

CREATE INDEX IF NOT EXISTS idx_comments_roots
    ON comments(post_id, created_at, id)
    WHERE parent_id IS NULL;