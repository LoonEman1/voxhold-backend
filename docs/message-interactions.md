# Replies, forwards and reactions

All endpoints require authentication and channel visibility. Reply targets must
belong to the same channel. Forward requests check both source and destination
access in the same SQLite transaction as the insert.

- `POST /api/v1/servers/{serverID}/channels/{channelID}/messages` accepts optional
  `reply_to_message_id`. Replies expose a short reference in `reply`; edits update
  the quote and deletions leave a deleted reference without the old text.
- `POST .../messages/forward` accepts `source_server_id`, `source_channel_id`,
  `source_message_id`, and optional `client_message_id`. The server copies the
  original text and attribution into an immutable snapshot. It never exposes
  private origin channel/server identifiers to destination readers. Recipients
  cannot edit a forward. Forwarded mentions do not ping people again.
- `PUT` / `DELETE .../messages/{messageID}/reactions/{emoji}` sets/removes the
  caller's reaction idempotently. URL-encode the native emoji. The catalog is
  restricted and each message supports twenty distinct reaction types.
- `GET .../messages/{messageID}/reactions/{emoji}?after_user_id=...` returns up to
  twenty users, `has_more` and `next_user_id`. These personalized responses are
  marked `Cache-Control: no-store`.

`client_message_id` is scoped to author and destination channel. Reusing it with
a different action, target or content returns 409. A retry of an accepted forward
returns the stored snapshot even if its source has since changed or disappeared.
The destination still requires current access. Clients keep failed sends in a
bounded outbox and reuse their original keys.

`message.reactions_changed` contains channel/message IDs, revision and reaction
counts with a recipient-specific `me` flag. Reactor identities are fetched on
demand, never broadcast with every reaction. Reactions are also included in all
history, sync, context, search and pinned-message reads. Slow-client queues
coalesce reaction updates and can request durable resynchronization.

Migration 24 adds metadata, reactions and revision triggers to the existing
message journal. Reaction removal on server departure and reply edits/deletions
therefore survive reconnects. Body and reaction snapshots can arrive out of
order; the frontend reconciles their revisions separately. Its bounded reactor
cache is scoped to login/channel and invalidated by reaction revision.

Replies to the recipient's messages trigger the default mentions notification
mode. Forwarded text does not trigger mentions; the all-messages mode still
delivers it as an ordinary notification.
