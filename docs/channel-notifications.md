# Text channel notifications / Уведомления текстовых каналов

Migration **000023** adds account-scoped channel preferences. Apply migrations
000022/000023 before deploying the matching frontend. Existing accounts default
to `mentions`; there is no backfill or notification replay for old history.

Authenticated `GET` / `PUT /api/v1/servers/{serverID}/channels/{channelID}/notifications`
read/replace the requesting member's preference. PUT accepts `{"mode":"all"}`,
`{"mode":"mentions"}`, or `{"mode":"none"}`. These endpoints require a visible
text channel; they never accept a different user ID. Responses use `no-store`.
Preference changes emit `notification.settings_updated` to that account's tabs.
Leaving a server or deleting a channel cascades its preference rows.

Committed new messages produce `notification.message` for eligible connected
accounts, including users browsing another channel/server. One indexed query
joins unique connected account IDs, the shared channel ACL view, current server
roles and per-channel preferences. Sender, inaccessible channels, voice-channel
chat, muted recipients, edits, history loading and idempotent send replays do not
generate alerts. Chat broadcast happens before notification selection. Notification
selection failures are logged without failing an already committed message.

Structured user/role mentions retain stable identities. The frontend displays
readable names in the composer, edit form, chat, pending outbox, search and pins.
Plain `@username` uses the exact case of the account name to avoid ambiguity
between existing case-distinct accounts. The `@` picker searches case-insensitively
and also handles names containing spaces. Built-in role mentions target exactly
`owner`, `admin`, or `member`; code spans, escaped text, email addresses and URL
paths do not ping. A recipient must independently have channel access.

The hub filters queued notifications on revocation. Under backpressure it retains
the latest notification per channel and can drop transient alerts to preserve
chat/permission events. The browser bounds its toast stack (4), dedup history (256)
and sound rate (one cue per 3 seconds). Web Locks plus a bounded local-storage
claim suppress duplicate delivery across tabs; unavailable browser storage uses
an in-memory/best-effort fallback. A fresh focused-channel heartbeat suppresses
alerts while that channel is actively read. Permission updates and revocations
also cancel pending alerts waiting on a cross-tab lock.

## Как пользоваться

ПКМ по **текстовому** каналу → **Уведомления**:

- **Все сообщения** — уведомлять о каждом новом сообщении.
- **Только упоминания** — о вас или вашей роли; это режим по умолчанию.
- **Ничего** — без уведомлений и звука из канала.

Введите `@`, выберите участника или роль стрелками/Enter либо мышью. Ваши
упоминания выделяются в сообщениях. Нажатие на уведомление открывает нужное
сообщение с повторной проверкой доступа.

Уведомления работают, пока Voxhold открыт и подключён. Системные уведомления
включаются отдельной кнопкой с разрешением браузера; закрытый браузер и офлайн
push в эту версию не входят. Звук учитывает общие настройки звуков интерфейса,
громкость и устройство вывода. В меню есть кнопка прослушивания сигнала.
