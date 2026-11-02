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
| `e2ee.keys` | `EventE2EEKeys` |
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

### Пакетная передача ключей и подтверждений

`ready.data.e2ee_batch: true` разрешает клиенту отправлять `e2ee.keys`:

```json
{"request_id":"42","type":"e2ee.keys","data":{"channel_id":7,"keys":[{"recipient_user_id":2,"recipient_device_id":"device-2","payload":"..."}]}}
```

В одном пакете от 1 до 32 получателей; для каждого действуют прежние ограничения
устройства и base64-payload (до 1 KiB). Некорректный элемент отклоняет весь пакет
до пересылки. Для каждого допустимого назначения hub проверяет текущее участие
обоих устройств в одном зашифрованном канале. Отправитель всегда берётся из
аутентифицированного соединения. Получателям по-прежнему приходит `e2ee.key`:
формат зашифрованного ключа и криптографических ACK не меняется.

Внешний бюджет WebSocket остаётся прежним. Дополнительный бюджет пересылки на
соединение считает каждое назначение: 120/с, burst 240; одиночные `e2ee.key` и
`e2ee.key_request` тоже его расходуют. Пакет не даёт неограниченного усиления
рассылки. Старые клиенты продолжают отправлять одиночные события. Новый клиент
использует пакеты только при полученном capability, иначе сохраняет прежний
транспорт и его ограничения.

### Отзыв устройств во время звонка

Явный отзыв и LRU-вытеснение устройства применяются к уже активному голосу.
Сервер удаляет невалидного участника и отправляет `voice.webrtc_closed` с причиной
`voice e2ee device was revoked`. Валидация повторяется внутри блокировки перехода
голосовой сессии пользователя, поэтому ранее начатый join не обходит отзыв.
База проверяется при переходах, а не при пересылке аудиопакетов.

В личных и групповых звонках удаляются только зашифрованные leases с отозванным
устройством; `conversation.call` сообщает оставшимся участникам новый состав.
Повторный heartbeat не восстанавливает такой lease. Обычные звонки и устройства
того же пользователя, которые не были отозваны, сохраняются.

