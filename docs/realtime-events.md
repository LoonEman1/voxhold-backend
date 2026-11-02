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
| `channel.activity` | `EventChannelActivity` |
| `e2ee.key` | `EventE2EEKey` |
| `e2ee.key_request` | `EventE2EEKeyRequest` |

## Непрочитанные каналы

`channel.activity` `{server_id, channel_id, message_id, author_user_id}` получают все участники
сервера, которым доступен канал, — без подписки на канал и без текста сообщения. Событие
отправляется только для сообщений верхнего уровня (не для ответов в ветках) и при медленном
клиенте склеивается до последнего `message_id` канала.

## Сквозное шифрование голоса (FEAT-13)

- `voice.join` принимает `e2ee_device_id`. Канал с `e2ee_required` пускает только клиентов с
  зарегистрированным ключом устройства, а обычный канал — только без него; ответ `voice.joined`
  содержит `e2ee_required`, а участники в `voice.snapshot`/`voice.participant_joined` — свой
  `e2ee_device_id`.
- `e2ee.key` `{channel_id, recipient_user_id, recipient_device_id, payload}` и
  `e2ee.key_request` (без `payload`) сервер пересылает одному соединению получателя, только если
  отправитель и получатель сейчас в одном зашифрованном канале. Получатель видит
  `{channel_id, sender_user_id, sender_device_id, sender_connection_id, payload}`. `payload` —
  base64 до 1 KiB; сервер его не разбирает и не логирует.
- Смена политики канала закрывает голос всех участников событием `voice.webrtc_closed` с
  причиной `voice channel encryption changed` и рассылает `channels.changed`.

