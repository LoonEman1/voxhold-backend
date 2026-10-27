# Channel access / Доступ к каналам

## Policy

Migration 000022 preserves existing channels as open. A restricted text or voice
channel is accessible only to current server members with an explicit user or
built-in role grant. Owner and admin always have access and manage the policy.
The supported roles remain `owner`, `admin`, `member`; this change does not add
custom roles or independent read/write/speak permission overrides.

`GET` / `PUT /api/v1/servers/{serverID}/channels/{channelID}/access` are authenticated,
manager-only, non-cacheable endpoints. PUT replaces the complete policy:

```json
{"restricted": true, "user_ids": [42], "roles": []}
```

`restricted` is required. User grants must reference current members of this
server; leaving/kicking removes the grant by a foreign-key cascade, so rejoining
does not restore it. Role and user grants form a union. Granting `member` includes
future members; an empty restricted policy permits only the owner/admins.

The shared `channel_accessible_members` view enforces the policy in channel
listing/rosters, message history/send/edit/delete/sync/context/pins/search,
read receipts and recordings (including previews and issued download tickets).
HTTP authorization and reads/writes use a consistent transaction or SQL statement.

The WebSocket hub caches each connected user's allowed channel IDs. ACL, role,
membership and channel creation changes refresh it, avoiding SQL on media packets
and ordinary broadcasts. Queued messages, snapshots and subsequent events are
filtered per recipient. Revocation removes subscriptions and closes voice and
stream sessions. A signalling gate covers SFU creation, preventing a delayed join
from recreating media after revocation. Loading errors fail closed and reconnect.
The same gate covers member removal, server deletion and channel deletion.
Removing membership or a server reloads cached permissions and purges queued
channel content before cleanup broadcasts are emitted.

`channels.changed` includes only `server_id`, allowing clients to refresh without
revealing hidden channel IDs/names. The frontend refreshes its roster, clears
revoked history/outbox/search entries and stops local media without auto-rejoining.

Deploy the backend migration before the new frontend. Previously downloaded files
and content already delivered to a client remain under that user's control.

## Настройка

Откройте меню канала → **Изменить канал** → **Доступ к каналу**. Выберите
«Закрытый канал», отметьте отдельных участников или роль и сохраните доступ.
Для канала только администраторов оставьте списки пустыми. Владелец и
администраторы сохраняют доступ всегда. Роль «Участник» открывает канал всем
обычным участникам, включая новых; для доступа конкретным людям её отмечать
не нужно. Настройка одинакова для текстовых и голосовых каналов.

Права проверяются сервером, включая прямые API-запросы. Изменения вступают в силу
при сохранении: недоступный канал исчезает из списка, активный звонок и просмотр
прекращаются, вкладка «Участники канала» показывает только допущенных людей.
Вкладка «Все участники» сохраняет общий список сервера.
