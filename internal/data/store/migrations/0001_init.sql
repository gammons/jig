CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    parent_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    agent TEXT NOT NULL,
    model TEXT NOT NULL,
    cwd TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_sessions_parent_updated ON sessions (parent_id, updated_at DESC, id DESC);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    agent TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    cache_read_tokens INTEGER NOT NULL,
    cache_write_tokens INTEGER NOT NULL,
    cost_usd REAL NOT NULL,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_messages_session_created ON messages (session_id, created_at, id);

CREATE TABLE parts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    kind TEXT NOT NULL,
    data_json TEXT NOT NULL
);

CREATE INDEX idx_parts_message_seq ON parts (message_id, seq);

CREATE TABLE todos (
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL,
    PRIMARY KEY (session_id, seq)
);
