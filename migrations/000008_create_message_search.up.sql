CREATE VIRTUAL TABLE message_search USING fts5(
    content,
    content = 'messages',
    content_rowid = 'id',
    tokenize = 'unicode61 remove_diacritics 2'
);

INSERT INTO message_search(rowid, content)
SELECT id, content
FROM messages;

CREATE TRIGGER message_search_after_insert
AFTER INSERT ON messages
BEGIN
    INSERT INTO message_search(rowid, content)
    VALUES (new.id, new.content);
END;

CREATE TRIGGER message_search_after_update
AFTER UPDATE OF content ON messages
BEGIN
    INSERT INTO message_search(message_search, rowid, content)
    VALUES ('delete', old.id, old.content);

    INSERT INTO message_search(rowid, content)
    VALUES (new.id, new.content);
END;

CREATE TRIGGER message_search_after_delete
AFTER DELETE ON messages
BEGIN
    INSERT INTO message_search(message_search, rowid, content)
    VALUES ('delete', old.id, old.content);
END;
