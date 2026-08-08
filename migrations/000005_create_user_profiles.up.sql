CREATE TABLE user_profiles (
    user_id INTEGER PRIMARY KEY,

    about TEXT NOT NULL DEFAULT ''
        check(length(about) <= 512),


    country_code TEXT
        CHECK (
            country_code IS NULL   
            OR length(country_code) = 2
        ),

    last_seen_at INTEGER
        CHECK (
            last_seen_at IS NULL
            OR last_seen_at >= 0
        ),

    updated_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);