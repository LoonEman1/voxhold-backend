# События WebSocket

Сгенерировано из `internal/realtime/event.go` (константы `EventType`). Полезная нагрузка (`data`) каждого события описана структурами с тегами `json` рядом с константами; примеры JSON — TODO (см. отчёт). Версия протокола: `ProtocolVersion` в том же файле.

Формат кадра: `{"request_id": "...", "type": "<событие>", "data": {...}}`.

| Событие | Имя константы |
|---|---|
| `auth` | `EventAuthenticate` |
| `ready` | `EventReady` |
| `error` | `EventError` |
| `ping` | `EventPing` |
| `pong` | `EventPong` |
| `channel.subscribe` | `EventChannelSubscribe` |
| `channel.subscribed` | `EventChannelSubscribed` |
| `channel.unsubscribe` | `EventChannelUnsubscribe` |
| `channel.unsubscribed` | `EventChannelUnsubscribed` |
| `server.member_removed` | `EventServerMemberRemoved` |
| `server.member_joined` | `EventServerMemberJoined` |
| `server.member_role_updated` | `EventServerMemberRoleUpdated` |
| `server.deleted` | `EventServerDeleted` |
| `invitation.received` | `EventInvitationReceived` |
| `channel.created` | `EventChannelCreated` |
| `channel.updated` | `EventChannelUpdated` |
| `channel.deleted` | `EventChannelDeleted` |
| `read.snapshot` | `EventReadSnapshot` |
| `channel.read_snapshot` | `EventChannelReadSnapshot` |
| `channel.read` | `EventChannelRead` |
| `message.created` | `EventMessageCreated` |
| `message.updated` | `EventMessageUpdated` |
| `message.deleted` | `EventMessageDeleted` |
| `notification.message` | `EventNotificationMessage` |
| `notification.settings_updated` | `EventNotificationSettingsUpdated` |
