# Community 2.3 / Общение и управление сервером

Upgrade the backend and migrations **27–30** before frontend **2.3.0**.
Back up SQLite and the entire persistent data volume, including `attachments/`
and `recordings/`. Rolling back these migrations drops new conversations,
subscriptions, roles and attachment metadata; restore the matching backup to
recover them. Existing unassigned members keep their previous permissions.

## Attachments and storage

Messages in channels and personal/group conversations accept up to ten attachments.
The browser supports file selection, drag/drop and clipboard images. Each upload
shows progress and has a retry action. A retry preserves the attachment ID and
message send key, including when the server accepted a request whose response was
lost. Uploads retry the complete file; partial bodies never become visible.

- `PUT /api/v1/channels/{channelID}/attachments/{32-hex-ID}?filename=...`
- `PUT /api/v1/conversations/{conversationID}/attachments/{32-hex-ID}?filename=...`
- `GET /api/v1/attachments/{ID}/content`

Upload raw bytes with bearer authentication, then send `attachment_ids` with the
message request. Staged files are visible only to their uploader. Binding checks
uploader, channel/conversation and deletion state in the message transaction.
Content always rechecks current channel access or conversation membership.
Authorization is never put in a download URL. Downloads support HTTP ranges.

The input limit is **64 MiB per file**. Supported raster images are decoded after
a 30-megapixel dimension check, resized to at most 2560 pixels per side and encoded
as JPEG quality 82, or PNG when transparency is present. MIME type comes from the
content. Other documents are downloads; MP4/WebM can play inline. Short videos
use the files budget. The original filename is retained after image conversion.

`ATTACHMENT_DIRECTORY` defaults to `data/attachments`. Keep it on the same
persistent filesystem as recordings and SQLite. The shared budget is 85% of
available disk space plus currently managed storage; the remaining 15% is reserved.
Allocations persist separately for `images`, `files` and `recordings`. Reservations
and allocation changes are serialized across upload and recording services.
New uploads above a category or the current overall budget are rejected; lowering
an allocation does not delete existing files. Setting a category to zero disables
new server copies in it. Call recordings retain their existing 24-hour retention.

Owner-only APIs:

- `GET /api/v1/instance/storage`: capacity, free space, reserve, per-category usage.
- `PUT /api/v1/instance/storage`: `{images,files,recordings}` in bytes.
- `POST /api/v1/instance/storage/preview`: `{category,before}`; Unix-second cutoff.
- `POST /api/v1/instance/storage/cleanup`: `{token}` from the preview.

A preview contains the exact eligible IDs, count and bytes and expires after five
minutes. Confirmation only deletes those IDs. Later uploads survive. An expired
or already used token requires another preview. Active recordings are excluded.
The instance owner can clean copies recorded by other members. Removed message
attachments remain as **«Вложение удалено сервером»**; metadata/revision changes
propagate to clients. Abandoned uploads and orphaned files are reclaimed after
a 24-hour grace period. Following an interrupted cleanup, request a fresh preview.

## Personal and group conversations

Select a participant and **«Написать»**, or create a conversation from the personal
messages section. A group contains at most 16 people including its creator.
The direct pair is unique regardless of which participant starts it.

- `GET/POST /api/v1/me/conversations`; POST `{user_ids,name}`.
- `GET/POST /api/v1/conversations/{ID}/messages`; GET supports `before` pagination.
- POST `{content,client_message_id,attachment_ids?}`. Reusing a key with different
  content/attachments returns 409; a retry returns the original message.
- `PUT /api/v1/conversations/{ID}/read`: `{message_id}`; monotonic and scoped.
- `DELETE /api/v1/conversations/{ID}/members/me`: leave and revoke history access.
- `GET/PUT /api/v1/me/message-privacy`: `{mode: "shared"|"everyone"|"none"}`.
- `PUT/DELETE /api/v1/me/blocks/{userID}`: block/unblock.

The default `shared` accepts people with a common server membership. Both creation
and each send recheck every recipient's privacy and blocks in either direction.
Groups do not bypass blocks. `conversation.message`, `conversation.changed` and
`conversation.read` are user-scoped WebSocket events. Unread state survives reloads.

## Inbox and text threads

**«Входящие»** gathers current accessible mentions, replies and unread comments in
followed discussions. Selecting an item opens its message and discussion. Queries
start from indexed notifications/subscriptions rather than scanning channel history.
Revoked/private channels never appear. Deleted messages cascade out of attention.

- `GET /api/v1/me/inbox?before={messageID}`: newest first, 50 items and `has_more`.
- `PUT /api/v1/me/inbox/{messageID}`: acknowledge the item.
- `GET/PUT /api/v1/channels/{channelID}/threads/{rootID}`: following and read state.
- PUT accepts optional `following` and `last_read_id`. Reading alone does not opt
  out of future automatic following; an explicit unsubscribe survives new replies.

Regular text messages now have an **«Обсудить»** action. Thread replies use explicit
`thread_root_id` on message creation; ordinary quoted replies remain in the main
channel. The root must be a top-level message in the same text channel. Replies
to thread comments must stay inside that root. Existing news-comment behavior is
preserved, including the news comments switch. Existing comment-list endpoints
also accept regular text roots. Participants/root authors automatically follow
unless they explicitly opted out. Thread read cursors are independent of main
channel reads. Pagination, reactions, editing, deletion and root counts share the
existing message synchronization.

## Custom roles

The owner creates names/colors, edits permissions, assigns multiple roles and
grants them access to private channels. Role badges appear beside authors and
in member profiles. The legacy owner/admin tiers remain protected.

| Bit | Permission |
| --- | --- |
| 1 | Read channels |
| 2 | Publish messages and replies |
| 4 | Join voice |
| 8 | Start streams (voice also required) |
| 16 | Moderate messages and manage pins |

Unassigned members use mask 15. Assigned roles replace that default and combine
by union. Owners/admins always have 31. Removing the last assigned role restores
the ordinary member default; the UI explains this before deleting a role.
News publishing also requires its existing publisher assignment. Read ACLs still
apply independently. Editing requires publish permission, even for an old author.
Revoking media rights stops the affected active voice/stream session. Renaming a
role or changing unrelated rights keeps allowed media running.

- `GET/POST /api/v1/servers/{serverID}/roles`.
- `PUT/DELETE /api/v1/servers/{serverID}/roles/{roleID}`.
- `PUT /api/v1/servers/{serverID}/members/{userID}/roles`: `{role_ids}`.
- `GET /api/v1/servers/{serverID}/roles/{roleID}/preview`.
- `GET/PUT /api/v1/servers/{serverID}/channels/{channelID}/role-grants`.

Preview evaluates a hypothetical ordinary member with that role, including legacy
member/private-role grants, without personal user grants or admin bypass. It does
not change the owner's session or grant access. Channel listings expose effective
`permissions` and `can_publish` for the caller.

## Verification and disposable browser data

Backend gates are full tests, vet, Linux race detector and the Go 1.27 leak gate.
Regressions cover scope/atomic attachment binding, retries, compression, quotas,
preview isolation, blocks, opt-outs, media revocation and migration rollback.

For the frontend fullstack test, create a new disposable directory from this
repository, then run the API against that database:

```sh
QA_DIRECTORY=/tmp/voxhold-community-qa go run ./scripts/qa-community
DATABASE_PATH=/tmp/voxhold-community-qa/voxhold.db go run ./cmd/api
```

These are public fixture accounts (`qaowner`, `qamember`, `qafriend`, password
`Community2.3-Test!`). The seed refuses an existing database. From frontend:

```sh
VOXHOLD_COMMUNITY_API=http://127.0.0.1:8080 npm run test:e2e:community
```

The test enables only its local Vite origin on this disposable instance. Never
seed or run this fixture against a production instance. Existing unit, UI and
performance suites remain separate release gates.

Interaction references supplied for this release: [Zulip inbox](https://zulip.com/help/inbox)
and [Mattermost threaded conversations](https://docs.mattermost.com/end-user-guide/collaborate/organize-conversations).

## Кратко по-русски

В 2.3 доступны вложения в каналах и личных беседах, просмотр картинок и видео,
прогресс с повтором загрузки, групповые беседы, блокировки, входящие и обсуждения
обычных текстовых сообщений. Владелец распределяет место между изображениями,
файлами и записями, рассчитывает очистку по периоду и подтверждает конкретный
набор файлов. Квоты и права проверяются сервером. После очистки остаются заглушки.
Роли настраиваются владельцем; кнопка предпросмотра показывает доступ роли без
изменения текущего аккаунта. Сначала обновляйте backend и миграции 27–30, затем
frontend. Для отката новых данных нужна согласованная резервная копия БД и файлов.
