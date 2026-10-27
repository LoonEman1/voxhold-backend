# Presence and channel access lists

`GET /api/v1/servers/{serverID}/channels/{channelID}/members` returns
`{"user_ids":[1,2]}` to an authenticated member with access to that channel.
The access check and roster read share a transaction. The response is private
(`Cache-Control: no-store`). IDs include offline users permitted to read/join,
not just users currently in a voice call. The current policy gives every
server member access to every channel, so this roster currently equals the
server membership. This change does not introduce per-user/channel ACL rules.

Presence transitions now persist `user_profiles.last_seen_at`. The first
connection marks a user online; only their final connection leaving marks them
offline. The optional `last_seen_at` field in `presence.updated` uses server Unix
seconds. SQL updates are monotonic and preserve profile fields. Updates happen
on presence transitions rather than on media events. A delayed storage write
cannot emit a stale offline status after a quick reconnect. Existing unknown
timestamps remain unknown until the user connects again; no history is invented.
Graceful shutdown records the disconnect; an abrupt process termination cannot
record an exact final instant.

No additional migration is needed beyond the existing profile schema.

## Русский

Новый API возвращает ID участников, которым доступен выбранный текстовый или
голосовой канал, включая офлайн-пользователей. Запрашивающий сам должен иметь
доступ. Проверка и чтение списка выполняются в одной транзакции. Сейчас доступ
общий для всех участников сервера: отдельных ограничений каналов эта функция
не добавляет, поэтому списки пока совпадают.

Время последнего онлайна сохраняется в существующем поле профиля и передаётся
событием присутствия. Закрытие одной из нескольких вкладок не переводит
пользователя в офлайн. Устаревшая запись не уменьшает время и не публикует
офлайн после нового подключения. Ранее неизвестное время остаётся неизвестным
до следующего подключения пользователя. Точный момент аварийного завершения
процесса восстановить невозможно.
