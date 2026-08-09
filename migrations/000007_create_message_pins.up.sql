CREATE TABLE message_pins (
    message_id INTEGER PRIMARY KEY,

    pinned_by_user_id INTEGER NOT NULL,

    pinned_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    FOREIGN KEY (message_id)
        REFERENCES messages(id)
        ON DELETE CASCADE,

    FOREIGN KEY (pinned_by_user_id)
        REFERENCES users(id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_message_pins_pinned_at
    ON message_pins(pinned_at DESC);
