CREATE TABLE channel_reads (
    channel_id INTEGER NOT NULL,

    user_id INTEGER NOT NULL,

    last_read_message_id INTEGER NOT NULL
        CHECK (last_read_message_id > 0),

    updated_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    PRIMARY KEY (channel_id, user_id),

    FOREIGN KEY (channel_id)
        REFERENCES channels(id)
        ON DELETE CASCADE,

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE INDEX idx_channel_reads_user_channel
    ON channel_reads(user_id, channel_id);
